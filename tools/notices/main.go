// Command notices prints THIRD_PARTY_NOTICES.md: the license texts of every module
// compiled into the Windows or Linux binaries. Run via `make notices`.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type module struct{ path, version, dir string }

func deps(goos string) (map[string]module, error) {
	cmd := exec.Command("go", "list", "-deps", "-f", "{{with .Module}}{{.Path}}\t{{.Version}}\t{{.Dir}}{{end}}", "./cmd/agendling")
	cmd.Env = append(os.Environ(), "GOOS="+goos)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list (%s): %w", goos, err)
	}
	res := map[string]module{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) == 3 && f[1] != "" { // main module has no version
			res[f[0]] = module{f[0], f[1], f[2]}
		}
	}
	return res, sc.Err()
}

func licenseFiles(dir string) []string {
	var res []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := strings.ToUpper(e.Name())
		if !e.IsDir() && (strings.HasPrefix(n, "LICENSE") || strings.HasPrefix(n, "LICENCE") ||
			strings.HasPrefix(n, "COPYING") || strings.HasPrefix(n, "NOTICE")) {
			res = append(res, filepath.Join(dir, e.Name()))
		}
	}
	return res
}

func main() {
	all := map[string]module{}
	for _, goos := range []string{"windows", "linux"} {
		m, err := deps(goos)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for k, v := range m {
			all[k] = v
		}
	}
	paths := make([]string, 0, len(all))
	for p := range all {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	fmt.Println("# Third-party notices")
	fmt.Println()
	fmt.Println("Agendling is distributed under the MIT License (see [LICENSE](LICENSE)). Its binaries")
	fmt.Println("include the following third-party Go modules, whose licenses are reproduced below.")
	fmt.Println("On Linux the app also links dynamically against the system's GTK 3 libraries (LGPL-2.1+),")
	fmt.Println("which are not bundled.")
	fmt.Println()
	for _, p := range paths {
		fmt.Printf("- [%s](#%s) %s\n", p, anchor(p), all[p].version)
	}
	for _, p := range paths {
		m := all[p]
		fmt.Printf("\n## %s\n\nVersion: %s\n", p, m.version)
		files := licenseFiles(m.dir)
		if len(files) == 0 {
			fmt.Println("\n_No license file found in the module; see its repository._")
			continue
		}
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			fmt.Printf("\n### %s\n\n```text\n%s\n```\n", filepath.Base(f), strings.TrimSpace(string(b)))
		}
	}
}

func anchor(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
