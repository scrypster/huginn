package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/scrypster/huginn/internal/skills"
)

func cmdSkill(args []string) error {
	if len(args) == 0 {
		return cmdSkillList()
	}
	switch args[0] {
	case "list":
		return cmdSkillList()
	case "search":
		q := ""
		if len(args) > 1 {
			q = strings.Join(args[1:], " ")
		}
		return cmdSkillSearch(q)
	case "info":
		if len(args) < 2 {
			return fmt.Errorf("usage: huginn skill info <name>")
		}
		return cmdSkillInfo(args[1])
	case "install":
		if len(args) < 2 {
			return fmt.Errorf("usage: huginn skill install <name|github:user/repo|./path>")
		}
		return cmdSkillInstall(args[1])
	case "enable":
		if len(args) < 2 {
			return fmt.Errorf("usage: huginn skill enable <name>")
		}
		return cmdSkillSetEnabled(args[1], true)
	case "disable":
		if len(args) < 2 {
			return fmt.Errorf("usage: huginn skill disable <name>")
		}
		return cmdSkillSetEnabled(args[1], false)
	case "uninstall":
		if len(args) < 2 {
			return fmt.Errorf("usage: huginn skill uninstall <name>")
		}
		return cmdSkillUninstall(args[1])
	case "update":
		target := ""
		if len(args) > 1 {
			target = args[1]
		}
		return cmdSkillUpdate(target)
	case "validate":
		if len(args) < 2 {
			return fmt.Errorf("usage: huginn skill validate <path>")
		}
		return cmdSkillValidate(args[1])
	case "create":
		return cmdSkillCreate()
	default:
		return fmt.Errorf("unknown skill subcommand: %s\nUsage: huginn skill [list|search|info|install|enable|disable|uninstall|update|validate|create]", args[0])
	}
}

func skillsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".huginn", "skills")
}

func skillManifest() (*skills.Manifest, error) {
	return skills.LoadManifest(filepath.Join(skillsDir(), "installed.json"))
}

func cmdSkillList() error {
	sdir := skillsDir()
	manifest, err := skillManifest()
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(sdir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read skills dir: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SKILL\tSTATUS\tSOURCE")

	found := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		s, err := skills.LoadMarkdownSkill(filepath.Join(sdir, entry.Name()))
		if err != nil {
			continue
		}
		status := "enabled"
		source := "local"
		if m := manifest.Get(s.Name()); m != nil {
			if !m.Enabled {
				status = "disabled"
			}
			source = m.Source
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", s.Name(), status, source)
		found++
	}
	w.Flush()

	if found == 0 {
		fmt.Println("No skills installed. Run: huginn skill search")
	}
	return nil
}

func cmdSkillSearch(query string) error {
	cachePath := skills.DefaultCachePath()
	entries, _, err := skills.LoadIndex(cachePath)
	if err != nil {
		return fmt.Errorf("load registry index: %w", err)
	}

	results := skills.SearchIndex(entries, query)
	if len(results) == 0 {
		fmt.Printf("No skills found matching %q\n", query)
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SKILL\tAUTHOR\tDESCRIPTION")
	for _, e := range results {
		desc := e.Description
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", e.Name, e.Author, desc)
	}
	w.Flush()
	return nil
}

func cmdSkillInfo(name string) error {
	// Check if installed locally first
	localPath := filepath.Join(skillsDir(), name+".md")
	if s, err := skills.LoadMarkdownSkill(localPath); err == nil {
		fmt.Printf("Name:   %s\n", s.Name())
		fmt.Printf("Author: %s\n", s.Author())
		fmt.Printf("Source:  %s\n", s.Source())
		fmt.Println()
		fmt.Println(s.SystemPromptFragment())
		if rc := s.RuleContent(); rc != "" {
			fmt.Println("\n## Rules")
			fmt.Println(rc)
		}
		return nil
	}

	// Fall back to registry index
	cachePath := skills.DefaultCachePath()
	entries, _, err := skills.LoadIndex(cachePath)
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}
	for _, e := range entries {
		if e.Name == name {
			fmt.Printf("Name:        %s\n", e.Name)
			fmt.Printf("Author:      %s\n", e.Author)
			fmt.Printf("Category:    %s\n", e.Category)
			fmt.Printf("Description: %s\n", e.Description)
			fmt.Printf("SourceURL:   %s\n", e.SourceURL)
			return nil
		}
	}
	return fmt.Errorf("skill %q not found locally or in registry", name)
}

func cmdSkillInstall(target string) error {
	sdir := skillsDir()
	if err := os.MkdirAll(sdir, 0755); err != nil {
		return fmt.Errorf("create skills dir: %w", err)
	}

	var (
		content []byte
		name    string
		source  string
	)

	switch {
	case strings.HasPrefix(target, "github:"):
		// github:user/repo — fetch SKILL.md from GitHub
		repo := strings.TrimPrefix(target, "github:")
		url := "https://raw.githubusercontent.com/" + repo + "/main/SKILL.md"
		fmt.Printf("⚠  This skill is not in the Huginn registry. Install anyway? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		if answer != "y" && answer != "Y" {
			fmt.Println("Aborted.")
			return nil
		}
		var err error
		content, err = fetchURL(url)
		if err != nil {
			return fmt.Errorf("fetch %s: %w", url, err)
		}
		source = target

	case strings.HasPrefix(target, "./") || strings.HasPrefix(target, "/"):
		// Local file path
		var err error
		content, err = os.ReadFile(target)
		if err != nil {
			return fmt.Errorf("read local skill: %w", err)
		}
		source = "local"

	default:
		// Registry skill name — look up in index
		cachePath := skills.DefaultCachePath()
		entries, _, err := skills.LoadIndex(cachePath)
		if err != nil {
			return fmt.Errorf("load registry: %w", err)
		}
		var found *skills.IndexEntry
		for i := range entries {
			if entries[i].Name == target {
				found = &entries[i]
				break
			}
		}
		if found == nil {
			return fmt.Errorf("skill %q not found in registry. Try: huginn skill search %s", target, target)
		}
		if found.SourceURL == "" {
			return fmt.Errorf("skill %q has no source_url in registry", target)
		}
		url := found.SourceURL
		content, err = fetchURL(url)
		if err != nil {
			return fmt.Errorf("fetch skill: %w", err)
		}
		source = "registry"
	}

	// Parse to get the name from frontmatter
	s, err := skills.ParseMarkdownSkillBytes(content)
	if err != nil {
		return fmt.Errorf("invalid SKILL.md: %w", err)
	}
	name = s.Name()

	// Write to skills dir
	destPath := filepath.Join(sdir, name+".md")
	if err := os.WriteFile(destPath, content, 0644); err != nil {
		return fmt.Errorf("write skill: %w", err)
	}

	// Update manifest
	manifest, _ := skills.LoadManifest(filepath.Join(sdir, "installed.json"))
	manifest.Upsert(skills.InstalledEntry{
		Name:    name,
		Source:  source,
		Enabled: true,
	})
	if err := manifest.Save(); err != nil {
		return fmt.Errorf("update manifest: %w", err)
	}

	fmt.Printf("✓ Installed skill %q\n", name)
	return nil
}

func cmdSkillSetEnabled(name string, enabled bool) error {
	sdir := skillsDir()
	manifest, err := skills.LoadManifest(filepath.Join(sdir, "installed.json"))
	if err != nil {
		return err
	}
	if !manifest.SetEnabled(name, enabled) {
		return fmt.Errorf("skill %q not found in installed.json", name)
	}
	if err := manifest.Save(); err != nil {
		return err
	}
	action := "enabled"
	if !enabled {
		action = "disabled"
	}
	fmt.Printf("Skill %q %s.\n", name, action)
	return nil
}

func cmdSkillUninstall(name string) error {
	sdir := skillsDir()

	// Remove file
	path := filepath.Join(sdir, name+".md")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove skill file: %w", err)
	}

	// Update manifest
	manifest, _ := skills.LoadManifest(filepath.Join(sdir, "installed.json"))
	manifest.Remove(name)
	if err := manifest.Save(); err != nil {
		return err
	}

	fmt.Printf("Skill %q uninstalled.\n", name)
	return nil
}

func cmdSkillUpdate(name string) error {
	sdir := skillsDir()
	cachePath := skills.DefaultCachePath()

	// Force-refresh the index
	entries, _, err := skills.FetchAndCacheIndex(cachePath)
	if err != nil {
		return fmt.Errorf("refresh registry: %w", err)
	}

	manifest, err := skills.LoadManifest(filepath.Join(sdir, "installed.json"))
	if err != nil {
		return err
	}

	updated := 0
	for _, e := range entries {
		if name != "" && e.Name != name {
			continue
		}
		// Only update registry-sourced skills
		m := manifest.Get(e.Name)
		if m == nil || m.Source != "registry" {
			continue
		}
		if e.SourceURL == "" {
			fmt.Printf("  ✗ %s: no source_url in registry\n", e.Name)
			continue
		}
		content, err := fetchURL(e.SourceURL)
		if err != nil {
			fmt.Printf("  ✗ %s: %v\n", e.Name, err)
			continue
		}
		if err := os.WriteFile(filepath.Join(sdir, e.Name+".md"), content, 0644); err != nil {
			fmt.Printf("  ✗ %s: write: %v\n", e.Name, err)
			continue
		}
		manifest.Upsert(skills.InstalledEntry{
			Name:    e.Name,
			Source:  "registry",
			Enabled: m.Enabled,
		})
		fmt.Printf("  ✓ %s\n", e.Name)
		updated++
	}
	if err := manifest.Save(); err != nil {
		return err
	}
	if updated == 0 {
		fmt.Println("All skills up to date.")
	}
	return nil
}

func cmdSkillValidate(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	s, err := skills.ParseMarkdownSkillBytes(data)
	if err != nil {
		fmt.Printf("✗ Invalid: %v\n", err)
		return nil
	}
	fmt.Printf("✓ Valid SKILL.md\n")
	fmt.Printf("  name:   %s\n", s.Name())
	if s.Author() != "" {
		fmt.Printf("  author:  %s\n", s.Author())
	}
	return nil
}

func cmdSkillCreate() error {
	template := `---
name: my-skill
author: your-name
source: local
huginn:
  priority: 5
---

Describe what this skill makes the agent an expert in.
Add specific instructions, idioms, and domain knowledge here.

## Rules

Add non-negotiable rules that the agent must follow.
`
	sdir := skillsDir()
	if err := os.MkdirAll(sdir, 0755); err != nil {
		return err
	}
	destPath := filepath.Join(sdir, "my-skill.md")
	if err := os.WriteFile(destPath, []byte(template), 0644); err != nil {
		return fmt.Errorf("write template: %w", err)
	}
	fmt.Printf("Created: %s\n", destPath)
	fmt.Println("Edit the file, then run: huginn skill validate " + destPath)
	return nil
}

// fetchURL downloads content from a URL.
func fetchURL(url string) ([]byte, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	const maxBytes = 10 << 20 // 10 MB
	lr := io.LimitReader(resp.Body, int64(maxBytes+1))
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, fmt.Errorf("response from %s exceeds 10 MB", url)
	}
	return data, nil
}
