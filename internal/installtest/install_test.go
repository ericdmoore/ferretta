// Package installtest verifies the shell installer with offline release fixtures.
package installtest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

type fixture struct {
	dir, home, bin, dest string
	env                  []string
}

func setup(t *testing.T, system, arch string) fixture {
	t.Helper()
	f := fixture{dir: t.TempDir()}
	f.home = filepath.Join(f.dir, "home with spaces")
	f.bin = filepath.Join(f.dir, "commands")
	f.dest = filepath.Join(f.home, ".local", "bin")
	for _, d := range []string{f.home, f.bin} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	// Archive bytes come from the test; fake curl never opens a socket.
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tw := tar.NewWriter(gz)
	payload := []byte("#!/bin/sh\nprintf 'fixture binary\\n'\n")
	if err := tw.WriteHeader(&tar.Header{Name: "ferretta", Mode: 0755, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(f.dir, "archive"), data.String(), 0600)
	var sums strings.Builder
	for _, osName := range []string{"darwin", "linux"} {
		for _, cpu := range []string{"amd64", "arm64"} {
			fmt.Fprintf(&sums, "%x  ferretta-v1.2.3-%s-%s.tar.gz\n", sha256.Sum256(data.Bytes()), osName, cpu)
		}
	}
	put(t, filepath.Join(f.dir, "checksums"), sums.String(), 0600)
	put(t, filepath.Join(f.bin, "uname"), "#!/bin/sh\ncase $1 in -s) echo "+system+";; -m) echo "+arch+";; esac\n", 0755)
	put(t, filepath.Join(f.bin, "id"), "#!/bin/sh\necho 1000\n", 0755)
	put(t, filepath.Join(f.bin, "sysctl"), "#!/bin/sh\necho 0\n", 0755)
	put(t, filepath.Join(f.bin, "curl"), `#!/bin/sh
set -eu
output=
for arg do
 if [ "${previous:-}" = --output ]; then output=$arg; fi
 previous=$arg
 url=$arg
done
printf '%s\n' "$url" >> "$FIXTURE/requests"
case "$url" in
 */releases/latest)
   [ "${NO_RELEASE:-}" != 1 ] || exit 22
   printf 'https://github.com/ericdmoore/ferretta/releases/tag/v1.2.3' ;;
 */ferretta-v1.2.3-checksums.txt) cp "$FIXTURE/checksums" "$output" ;;
 */ferretta-v1.2.3-*.tar.gz) cp "$FIXTURE/archive" "$output" ;;
 https://ollama.com/install.sh) printf '#!/bin/sh\nprintf installed > "$FIXTURE/ollama-installed"\n' > "$output" ;;
 *) exit 22 ;;
esac
`, 0755)
	// These sentinels record optional operations. The default install must not call them.
	put(t, filepath.Join(f.bin, "uv"), "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$FIXTURE/uv-called\"\n", 0755)
	put(t, filepath.Join(f.bin, "ollama"), "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$FIXTURE/ollama-called\"\n", 0755)
	put(t, filepath.Join(f.bin, "sudo"), "#!/bin/sh\nexit 99\n", 0755)
	f.env = []string{"PATH=" + f.bin + ":/usr/bin:/bin:/usr/sbin:/sbin", "HOME=" + f.home, "TMPDIR=" + f.dir, "FIXTURE=" + f.dir}
	return f
}
func (f fixture) run(args ...string) (string, error) {
	script, _ := filepath.Abs("../../install.sh")
	cmd := exec.Command("/bin/sh", append([]string{script}, args...)...)
	cmd.Env = f.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}
func TestPlatformsAndExplicitOptions(t *testing.T) {
	for _, tc := range []struct{ system, arch, target string }{{"Darwin", "arm64", "darwin-arm64"}, {"Darwin", "x86_64", "darwin-amd64"}, {"Linux", "aarch64", "linux-arm64"}, {"Linux", "x86_64", "linux-amd64"}} {
		t.Run(tc.target, func(t *testing.T) {
			t.Parallel()
			f := setup(t, tc.system, tc.arch)
			out, err := f.run("--no-input")
			if err != nil {
				t.Fatal(out, err)
			}
			info, err := os.Stat(filepath.Join(f.dest, "ferretta"))
			if err != nil || info.Mode().Perm() != 0755 {
				t.Fatal(info, err)
			}
			requests, _ := os.ReadFile(filepath.Join(f.dir, "requests"))
			if !strings.Contains(string(requests), "v1.2.3-"+tc.target+".tar.gz") {
				t.Fatal(string(requests))
			}
			if !strings.Contains(out, "Add this directory") {
				t.Fatal("missing PATH guidance", out)
			}
			for _, name := range []string{"uv-called", "ollama-called", "ollama-installed"} {
				if _, err := os.Stat(filepath.Join(f.dir, name)); !os.IsNotExist(err) {
					t.Fatal("unselected component executed", name)
				}
			}
			put(t, filepath.Join(f.dest, "ferretta"), "old", 0755)
			if out, err = f.run("--no-input"); err != nil {
				t.Fatal(out, err)
			}
			installed, _ := os.ReadFile(filepath.Join(f.dest, "ferretta"))
			if string(installed) == "old" {
				t.Fatal("upgrade did not replace binary")
			}
			stages, _ := filepath.Glob(filepath.Join(f.dest, ".ferretta.*"))
			if len(stages) != 0 {
				t.Fatal("stage not cleaned")
			}
		})
	}
	t.Run("pinned custom directory", func(t *testing.T) {
		f := setup(t, "Linux", "amd64")
		dest := filepath.Join(f.dir, "custom bin")
		f.env = append(f.env, "FERRETTA_VERSION=v1.2.3", "FERRETTA_INSTALL_DIR="+dest)
		out, err := f.run("--no-input")
		if err != nil {
			t.Fatal(out, err)
		}
		requests, _ := os.ReadFile(filepath.Join(f.dir, "requests"))
		if strings.Contains(string(requests), "latest") {
			t.Fatal("pinned version resolved latest")
		}
		if _, err := os.Stat(filepath.Join(dest, "ferretta")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Rosetta", func(t *testing.T) {
		f := setup(t, "Darwin", "x86_64")
		put(t, filepath.Join(f.bin, "sysctl"), "#!/bin/sh\necho 1\n", 0755)
		out, err := f.run("--no-input")
		if err != nil || !strings.Contains(out, "darwin/arm64") {
			t.Fatal(out, err)
		}
	})
}
func TestFailuresPreserveInstalledBinary(t *testing.T) {
	for _, mode := range []string{"checksum", "missing checksum", "duplicate checksum", "empty", "no release", "bad version", "bad OS", "bad arch", "relative path", "directory", "symlink", "archive", "argument"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := setup(t, "Linux", "x86_64")
			if err := os.MkdirAll(f.dest, 0755); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(f.dest, "ferretta")
			put(t, target, "previous binary", 0755)
			args := []string{"--no-input"}
			switch mode {
			case "checksum":
				put(t, filepath.Join(f.dir, "archive"), "corrupt", 0600)
			case "missing checksum":
				put(t, filepath.Join(f.dir, "checksums"), "", 0600)
			case "duplicate checksum":
				data, _ := os.ReadFile(filepath.Join(f.dir, "checksums"))
				put(t, filepath.Join(f.dir, "checksums"), string(data)+string(data), 0600)
			case "empty", "archive":
				payload := ""
				if mode == "archive" {
					payload = "invalid archive"
				}
				put(t, filepath.Join(f.dir, "archive"), payload, 0600)
				put(t, filepath.Join(f.dir, "checksums"), fmt.Sprintf("%x  ferretta-v1.2.3-linux-amd64.tar.gz\n", sha256.Sum256([]byte(payload))), 0600)
			case "no release":
				f.env = append(f.env, "NO_RELEASE=1")
			case "bad version":
				f.env = append(f.env, "FERRETTA_VERSION=v1/../../bad")
			case "bad OS":
				put(t, filepath.Join(f.bin, "uname"), "#!/bin/sh\necho Windows\n", 0755)
			case "bad arch":
				put(t, filepath.Join(f.bin, "uname"), "#!/bin/sh\ncase $1 in -s) echo Linux;; *) echo riscv64;; esac\n", 0755)
			case "relative path":
				f.env = append(f.env, "FERRETTA_INSTALL_DIR=relative")
			case "directory":
				f.env = append(f.env, "FERRETTA_INSTALL_DIR="+target)
			case "symlink":
				if err := os.Rename(target, target+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target+".real", target); err != nil {
					t.Fatal(err)
				}
			case "argument":
				args = append(args, "--unknown")
			}
			out, err := f.run(args...)
			if err == nil {
				t.Fatal("expected failure", out)
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "previous binary" {
				t.Fatal("existing installation damaged", err, string(data))
			}
			temps, _ := filepath.Glob(filepath.Join(f.dir, "ferretta-install.*"))
			if len(temps) > 0 {
				t.Fatal("temporary downloads not cleaned")
			}
		})
	}
}
func TestExplicitComponentSelection(t *testing.T) {
	f := setup(t, "Linux", "amd64")
	out, err := f.run("--no-input", "--with-ollama", "--with-litellm", "--pull-model", "qwen3:4b-thinking")
	if err != nil {
		t.Fatal(out, err)
	}
	for name, want := range map[string]string{"uv-called": "tool install litellm[proxy]", "ollama-called": "pull qwen3:4b-thinking"} {
		got, err := os.ReadFile(filepath.Join(f.dir, name))
		if err != nil || strings.TrimSpace(string(got)) != want {
			t.Fatal(name, string(got), err)
		}
	}
	if _, err := os.Stat(filepath.Join(f.dir, "ollama-installed")); !os.IsNotExist(err) {
		t.Fatal("existing Ollama replaced")
	}
	if err := os.Remove(filepath.Join(f.bin, "ollama")); err != nil {
		t.Fatal(err)
	}
	out, err = f.run("--no-input", "--with-ollama")
	if err != nil {
		t.Fatal(out, err)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "ollama-installed")); err != nil {
		t.Fatal("official installer not executed", err)
	}
	if err := os.Remove(filepath.Join(f.bin, "uv")); err != nil {
		t.Fatal(err)
	}
	out, err = f.run("--no-input", "--with-litellm")
	if err == nil || !strings.Contains(out, "Ferretta installed; install uv first") {
		t.Fatal(out, err)
	}
}

func TestPipedInstaller(t *testing.T) {
	f := setup(t, "Linux", "x86_64")
	data, err := os.ReadFile("../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", "-s", "--", "--no-input")
	cmd.Env = f.env
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(string(out), err)
	}
	if _, err := os.Stat(filepath.Join(f.dest, "ferretta")); err != nil {
		t.Fatal(err)
	}
}
