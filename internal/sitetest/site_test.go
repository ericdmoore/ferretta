//go:build site

// Package sitetest checks the generated public artifact, without network calls.
package sitetest

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPublicSite(t *testing.T) {
	root, err := filepath.Abs("../../bin/site")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	published, err := os.ReadFile(filepath.Join(root, "install.sh"))
	if err != nil || !bytes.Equal(source, published) {
		t.Fatal("published installer differs from tested source", err)
	}
	cname, err := os.ReadFile(filepath.Join(root, "CNAME"))
	if err != nil || strings.TrimSpace(string(cname)) != "ferretta.cc" {
		t.Fatal("wrong domain", err)
	}
	for _, path := range []string{"index.html", "docs/index.html", "docs/install/index.html", "docs/getting-started/index.html", "docs/github-app-auth/index.html", "docs/configuration/index.html", "docs/service/index.html", "docs/arch/index.html"} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Fatal("missing page", path, err)
		}
	}
	links := regexp.MustCompile(`(?:href|src)=(?:"([^"]+)"|'([^']+)'|([^\s>]+))`)
	count := 0
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.Contains(path, ".ferretta") {
				t.Error("private state in public tree", path)
			}
			return nil
		}
		if !strings.HasSuffix(path, ".html") {
			return nil
		}
		count++
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range links.FindAllSubmatch(body, -1) {
			raw := ""
			for _, part := range match[1:] {
				if len(part) > 0 {
					raw = string(part)
					break
				}
			}
			u, err := url.Parse(raw)
			if err != nil {
				t.Error(path, raw, err)
				continue
			}
			if u.IsAbs() || u.Host != "" || u.Path == "" {
				continue
			}
			target := filepath.Join(filepath.Dir(path), filepath.FromSlash(u.Path))
			if strings.HasPrefix(u.Path, "/") {
				target = filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(u.Path, "/")))
			}
			if !strings.HasPrefix(target, root+string(os.PathSeparator)) && target != root {
				t.Error("link escapes site", path, raw)
				continue
			}
			info, err := os.Stat(target)
			if err == nil && info.IsDir() {
				_, err = os.Stat(filepath.Join(target, "index.html"))
			}
			if err != nil {
				t.Error("broken local link", path, raw, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count < 8 {
		t.Fatal("site unexpectedly empty", count)
	}
}
