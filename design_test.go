package main

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The design documents are cited by document and heading
// (AGENTS.md, Working conventions), so a renamed heading or a removed
// document must fail here rather than leave a citation pointing at nothing.

const designDir = "doc/design"

var (
	// A citation, possibly wrapped over several comment lines: the document,
	// then optionally a comma and the heading in double quotes.
	citationRe = regexp.MustCompile(`doc/design/([a-z0-9_/-]+\.md)(?:,\s*"([^"]+)")?`)
	// A Markdown link to a .md file, with an optional anchor.
	linkRe = regexp.MustCompile(`\]\(([^)\s#]*\.md)(?:#([^)\s]+))?\)`)
	// A bare Markdown file name, not preceded by a directory,
	// nor opening a string literal (a test's file name, not a reference).
	bareRe = regexp.MustCompile(`(?:^|[^A-Za-z0-9_./"-])([A-Za-z0-9_-]+\.md)\b`)
	// What a comment line starts with, stripped before lines are joined.
	leaderRe  = regexp.MustCompile(`^\s*(//|#|\*|-)?\s*`)
	headingRe = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*$`)
	slugDrop  = regexp.MustCompile(`[^\p{L}\p{N} _-]`)
)

func TestDesignCitations(t *testing.T) {
	headings := map[string]map[string]bool{} // document → heading → true
	anchors := map[string]map[string]bool{}  // document → anchor → true
	err := filepath.WalkDir(designDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".md" {
			return err
		}
		h, a, err := readHeadings(path)
		headings[filepath.ToSlash(path)], anchors[filepath.ToSlash(path)] = h, a
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	// Every Markdown file's name, so a bare name in code can be checked:
	// one naming a file that exists is a plain reference, not a citation.
	mdNames := map[string]bool{}
	err = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && filepath.ToSlash(path) == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() && filepath.Ext(path) == ".md" {
			mdNames[filepath.Base(path)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch filepath.ToSlash(path) {
			case ".git", ".claude", "vendor", "dist", "doc/md", "doc/man":
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".go", ".star", ".yaml", ".yml", ".md":
		default:
			return nil
		}
		if filepath.Base(path) == "design_test.go" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := flatten(string(raw))

		for _, m := range citationRe.FindAllStringSubmatch(text, -1) {
			doc := designDir + "/" + m[1]
			hs, ok := headings[doc]
			if !ok {
				t.Errorf("%s: cites %s, which does not exist", path, doc)
				continue
			}
			if m[2] != "" && !hs[normalize(m[2])] {
				t.Errorf("%s: cites %s, %q: no such heading", path, doc, m[2])
			}
		}

		if filepath.Ext(path) != ".md" {
			// A bare name is not a citation; one that names no file in the
			// repository is a dead one, such as "(type-change.md, TC8)".
			for _, m := range bareRe.FindAllStringSubmatch(text, -1) {
				if !mdNames[m[1]] {
					t.Errorf("%s: names %s, which is no file in the repository; cite doc/design/<aspect>.md, \"<heading>\"", path, m[1])
				}
			}
			return nil
		}
		for _, m := range linkRe.FindAllStringSubmatch(text, -1) {
			if strings.Contains(m[1], "://") {
				continue
			}
			doc := filepath.ToSlash(filepath.Join(filepath.Dir(path), m[1]))
			if !strings.HasPrefix(doc, designDir+"/") {
				continue
			}
			as, ok := anchors[doc]
			if !ok {
				t.Errorf("%s: links to %s, which does not exist", path, doc)
				continue
			}
			if m[2] != "" && !as[m[2]] {
				t.Errorf("%s: links to %s#%s: no such heading", path, doc, m[2])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// readHeadings returns a document's headings, normalized, and their anchors
// as GitHub derives them. Lines inside a code fence are not headings.
func readHeadings(path string) (map[string]bool, map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	headings, anchors := map[string]bool{}, map[string]bool{}
	seen := map[string]int{}
	fenced := false
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		m := headingRe.FindStringSubmatch(line)
		if fenced || m == nil {
			continue
		}
		headings[normalize(m[1])] = true
		slug := strings.ReplaceAll(slugDrop.ReplaceAllString(strings.ToLower(m[1]), ""), " ", "-")
		if n := seen[slug]; n > 0 {
			anchors[slug+"-"+strconv.Itoa(n)] = true
		} else {
			anchors[slug] = true
		}
		seen[slug]++
	}
	return headings, anchors, s.Err()
}

// flatten joins a file's lines, dropping each one's comment leader,
// so a citation wrapped over lines reads as one.
func flatten(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = leaderRe.ReplaceAllString(l, "")
	}
	return strings.Join(lines, " ")
}

func normalize(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
