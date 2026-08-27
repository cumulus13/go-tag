package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Version is set at build time via -ldflags "-X main.Version=x.y.z" (see
// .github/workflows/release.yml). Kept as a var (not const) so it can be
// overridden by the linker per release build, matching git-remote-color.
var Version = "1.0.0"

const fieldSep = "\x1f" // ASCII unit separator — never appears in git metadata

// ─── Config ─────────────────────────────────────────────────────────────────

type Config struct {
	TagName     string `json:"tag_name"`
	Annotated   string `json:"annotated"`
	Lightweight string `json:"lightweight"`
	Hash        string `json:"hash"`
	Date        string `json:"date"`
	Message     string `json:"message"`
	Tagger      string `json:"tagger"`
	Header      string `json:"header"`
	Remote      string `json:"remote"`
	Success     string `json:"success"`
	Error       string `json:"error"`
	Warning     string `json:"warning"`
	Muted       string `json:"muted"`
	// DefaultSort controls the default `for-each-ref` sort key used by the
	// list view when --sort isn't given on the command line.
	DefaultSort string `json:"default_sort"`
	// DefaultRemote is the remote used by `push` when none is given.
	DefaultRemote string `json:"default_remote"`
}

func defaultConfig() Config {
	return Config{
		TagName:       "#FFFF00",
		Annotated:     "#00FFAA",
		Lightweight:   "#AAAAAA",
		Hash:          "#00AAFF",
		Date:          "#AAAAFF",
		Message:       "#DDDDDD",
		Tagger:        "#FFAAFF",
		Header:        "#FF6B6B",
		Remote:        "#55AA00",
		Success:       "#55FF55",
		Error:         "#FF5555",
		Warning:       "#FFE66D",
		Muted:         "#888888",
		DefaultSort:   "-creatordate",
		DefaultRemote: "origin",
	}
}

func mergeConfig(dst *Config, src Config) {
	set := func(d *string, s string) {
		if s != "" {
			*d = s
		}
	}
	set(&dst.TagName, src.TagName)
	set(&dst.Annotated, src.Annotated)
	set(&dst.Lightweight, src.Lightweight)
	set(&dst.Hash, src.Hash)
	set(&dst.Date, src.Date)
	set(&dst.Message, src.Message)
	set(&dst.Tagger, src.Tagger)
	set(&dst.Header, src.Header)
	set(&dst.Remote, src.Remote)
	set(&dst.Success, src.Success)
	set(&dst.Error, src.Error)
	set(&dst.Warning, src.Warning)
	set(&dst.Muted, src.Muted)
	set(&dst.DefaultSort, src.DefaultSort)
	set(&dst.DefaultRemote, src.DefaultRemote)
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// configCandidates returns config file search locations, in priority order:
// $TAG_CONFIG env var, executable dir, cwd, platform config dir, home dir.
func configCandidates() []string {
	exe, _ := os.Executable()
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	exeBase := strings.TrimSuffix(filepath.Base(exe), filepath.Ext(exe))
	exeDir := filepath.Dir(exe)

	names := uniqueStrings([]string{
		exeBase + ".json",
		"tag.json",
		".tag.json",
	})

	var candidates []string
	if env := strings.TrimSpace(os.Getenv("TAG_CONFIG")); env != "" {
		candidates = append(candidates, env)
	}
	for _, name := range names {
		candidates = append(candidates, filepath.Join(exeDir, name))
	}
	if cwd, err := os.Getwd(); err == nil {
		for _, name := range names {
			candidates = append(candidates, filepath.Join(cwd, name))
		}
	}
	if configDir, err := os.UserConfigDir(); err == nil {
		for _, name := range names {
			candidates = append(candidates,
				filepath.Join(configDir, "tag", name),
				filepath.Join(configDir, name),
			)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, name := range names {
			candidates = append(candidates, filepath.Join(home, name))
		}
	}
	if runtime.GOOS != "windows" {
		if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
			for _, name := range names {
				candidates = append(candidates, filepath.Join(xdg, "tag", name))
			}
		}
	}
	return uniqueStrings(candidates)
}

var loadedConfigPath string

func loadConfig() Config {
	cfg := defaultConfig()
	for _, path := range configCandidates() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var partial Config
		if err := json.Unmarshal(data, &partial); err != nil {
			fmt.Fprintf(os.Stderr, "%s malformed config at %s: %v\n", warn("⚠"), path, err)
			continue
		}
		mergeConfig(&cfg, partial)
		loadedConfigPath = path
		break
	}
	return cfg
}

// ─── Color ──────────────────────────────────────────────────────────────────

var noColor = os.Getenv("NO_COLOR") != "" || os.Getenv("TAG_NO_COLOR") != ""

func color(hex, text string) string {
	if noColor || hex == "" || text == "" {
		return text
	}
	hex = strings.TrimSpace(hex)
	if !strings.HasPrefix(hex, "#") || (len(hex) != 7 && len(hex) != 4) {
		return text
	}
	if len(hex) == 4 {
		hex = "#" + string([]byte{hex[1], hex[1], hex[2], hex[2], hex[3], hex[3]})
	}
	var r, g, b int
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return text
	}
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", r, g, b, text)
}

// Bootstrap error-message colorizers that don't depend on a loaded Config —
// used before/around config loading and for early argument errors.
func warn(s string) string  { return color("#FFE66D", s) }
func fail(s string) string  { return color("#FF5555", s) }
func ok(s string) string    { return color("#55FF55", s) }
func muted(s string) string { return color("#888888", s) }

// ─── Args ───────────────────────────────────────────────────────────────────

type Command int

const (
	CmdList Command = iota
	CmdAdd
	CmdDelete
	CmdShow
	CmdPush
	CmdVerify
	CmdRename
	CmdConfig
	CmdHelp
	CmdVersion
)

type Args struct {
	Dir     string
	Command Command

	// list
	Pattern    string
	Sort       string
	NLines     int
	ShowAll    bool
	PointsAt   string
	Contains   string
	NoContains string
	Merged     string
	NoMerged   string

	// add
	NewTagName  string
	Message     string
	MessageFile string
	Annotated   bool
	Signed      bool
	KeyID       string
	Force       bool
	At          string // commit/object to tag (default HEAD)

	// delete / verify (multiple names)
	TagNames []string

	// show
	ShowName string

	// push
	PushRemote string
	PushAll    bool
	PushTags   []string

	// rename
	OldName string
	NewName string

	// config
	ConfigSub string // "show" | "path"
}

var commandWords = map[string]Command{
	"list": CmdList, "ls": CmdList,
	"add": CmdAdd, "create": CmdAdd, "new": CmdAdd,
	"delete": CmdDelete, "del": CmdDelete, "rm": CmdDelete, "remove": CmdDelete,
	"show": CmdShow, "info": CmdShow,
	"push": CmdPush, "publish": CmdPush,
	"verify": CmdVerify,
	"rename": CmdRename, "mv": CmdRename,
	"config":  CmdConfig,
	"help":    CmdHelp,
	"version": CmdVersion,
}

func pathLike(s string) bool {
	return strings.HasPrefix(s, ".") || strings.HasPrefix(s, "/") ||
		strings.HasPrefix(s, "~") || strings.Contains(s, string(filepath.Separator))
}

// looksLikeDir reports whether arg should be consumed as WORKING_DIR: it's
// either obviously path-shaped, or it exists on disk as a directory, and it
// is NOT a recognized command word (command words always win).
func looksLikeDir(arg string) bool {
	if _, isCmd := commandWords[arg]; isCmd {
		return false
	}
	if strings.HasPrefix(arg, "-") {
		return false
	}
	if pathLike(arg) {
		return true
	}
	if fi, err := os.Stat(arg); err == nil && fi.IsDir() {
		return true
	}
	return false
}

func parseArgs(argv []string) (Args, error) {
	a := Args{Dir: ".", Command: CmdList, PushRemote: ""}
	i := 0

	// 1. Optional global -C/--dir flag (unambiguous, git-style), checked
	//    anywhere in the leading position for convenience.
	for i < len(argv) && (argv[i] == "-C" || argv[i] == "--dir") {
		if i+1 >= len(argv) {
			return a, fmt.Errorf("%s requires a directory argument", argv[i])
		}
		a.Dir = argv[i+1]
		argv = append(argv[:i], argv[i+2:]...)
	}

	// 2. Optional positional WORKING_DIR (only if it's unambiguous).
	if len(argv) > i && looksLikeDir(argv[i]) {
		a.Dir = argv[i]
		i++
	}

	// 3. Optional command word.
	if len(argv) > i {
		if cmd, isCmd := commandWords[argv[i]]; isCmd {
			a.Command = cmd
			i++
		} else if strings.HasPrefix(argv[i], "-h") || argv[i] == "--help" {
			a.Command = CmdHelp
			i++
		} else if argv[i] == "-v" || argv[i] == "--version" {
			a.Command = CmdVersion
			i++
		}
	}

	rest := argv[i:]

	switch a.Command {
	case CmdList:
		return parseListArgs(a, rest)
	case CmdAdd:
		return parseAddArgs(a, rest)
	case CmdDelete:
		return parseDeleteArgs(a, rest)
	case CmdShow:
		return parseShowArgs(a, rest)
	case CmdPush:
		return parsePushArgs(a, rest)
	case CmdVerify:
		return parseVerifyArgs(a, rest)
	case CmdRename:
		return parseRenameArgs(a, rest)
	case CmdConfig:
		return parseConfigArgs(a, rest)
	default:
		return a, nil
	}
}

func nextVal(rest []string, i int, flag string) (string, error) {
	if i+1 >= len(rest) {
		return "", fmt.Errorf("%s requires a value", flag)
	}
	return rest[i+1], nil
}

func parseListArgs(a Args, rest []string) (Args, error) {
	a.Sort = ""
	a.NLines = -1
	for i := 0; i < len(rest); i++ {
		arg := rest[i]
		switch {
		case arg == "-a" || arg == "--all":
			a.ShowAll = true
		case arg == "--sort":
			v, err := nextVal(rest, i, arg)
			if err != nil {
				return a, err
			}
			a.Sort = v
			i++
		case strings.HasPrefix(arg, "--sort="):
			a.Sort = strings.TrimPrefix(arg, "--sort=")
		case arg == "-n":
			a.NLines = 1
		case strings.HasPrefix(arg, "-n") && len(arg) > 2:
			n, err := strconv.Atoi(arg[2:])
			if err != nil {
				return a, fmt.Errorf("invalid -n value: %q", arg[2:])
			}
			a.NLines = n
		case arg == "--points-at":
			v, err := nextVal(rest, i, arg)
			if err != nil {
				return a, err
			}
			a.PointsAt = v
			i++
		case strings.HasPrefix(arg, "--points-at="):
			a.PointsAt = strings.TrimPrefix(arg, "--points-at=")
		case arg == "--contains":
			v, err := nextVal(rest, i, arg)
			if err != nil {
				return a, err
			}
			a.Contains = v
			i++
		case strings.HasPrefix(arg, "--contains="):
			a.Contains = strings.TrimPrefix(arg, "--contains=")
		case arg == "--no-contains":
			v, err := nextVal(rest, i, arg)
			if err != nil {
				return a, err
			}
			a.NoContains = v
			i++
		case strings.HasPrefix(arg, "--no-contains="):
			a.NoContains = strings.TrimPrefix(arg, "--no-contains=")
		case arg == "--merged":
			v, err := nextVal(rest, i, arg)
			if err != nil {
				return a, err
			}
			a.Merged = v
			i++
		case strings.HasPrefix(arg, "--merged="):
			a.Merged = strings.TrimPrefix(arg, "--merged=")
		case arg == "--no-merged":
			v, err := nextVal(rest, i, arg)
			if err != nil {
				return a, err
			}
			a.NoMerged = v
			i++
		case strings.HasPrefix(arg, "--no-merged="):
			a.NoMerged = strings.TrimPrefix(arg, "--no-merged=")
		case strings.HasPrefix(arg, "-"):
			return a, fmt.Errorf("unknown flag for list: %s", arg)
		default:
			if a.Pattern != "" {
				return a, fmt.Errorf("unexpected extra argument: %s", arg)
			}
			a.Pattern = arg
		}
	}
	return a, nil
}

func parseAddArgs(a Args, rest []string) (Args, error) {
	a.At = "HEAD"
	var positional []string
	for i := 0; i < len(rest); i++ {
		arg := rest[i]
		switch {
		case arg == "-a" || arg == "--annotate":
			a.Annotated = true
		case arg == "-s" || arg == "--sign":
			a.Signed = true
			a.Annotated = true
		case arg == "-f" || arg == "--force":
			a.Force = true
		case arg == "-u" || arg == "--local-user":
			v, err := nextVal(rest, i, arg)
			if err != nil {
				return a, err
			}
			a.KeyID = v
			a.Annotated = true
			i++
		case arg == "-m" || arg == "--message":
			v, err := nextVal(rest, i, arg)
			if err != nil {
				return a, err
			}
			a.Message = v
			a.Annotated = true
			i++
		case strings.HasPrefix(arg, "-m="), strings.HasPrefix(arg, "--message="):
			a.Message = arg[strings.Index(arg, "=")+1:]
			a.Annotated = true
		case arg == "-F" || arg == "--file":
			v, err := nextVal(rest, i, arg)
			if err != nil {
				return a, err
			}
			a.MessageFile = v
			a.Annotated = true
			i++
		case strings.HasPrefix(arg, "-"):
			return a, fmt.Errorf("unknown flag for add: %s", arg)
		default:
			positional = append(positional, arg)
		}
	}
	switch len(positional) {
	case 0:
		return a, fmt.Errorf("add requires a tag name, e.g. `tag add v1.0.0 -m \"release\"`")
	case 1:
		a.NewTagName = positional[0]
	case 2:
		a.NewTagName = positional[0]
		a.At = positional[1]
	default:
		return a, fmt.Errorf("too many arguments for add: %s", strings.Join(positional[2:], " "))
	}
	return a, nil
}

func parseDeleteArgs(a Args, rest []string) (Args, error) {
	for _, arg := range rest {
		if strings.HasPrefix(arg, "-") {
			return a, fmt.Errorf("unknown flag for delete: %s", arg)
		}
		a.TagNames = append(a.TagNames, arg)
	}
	if len(a.TagNames) == 0 {
		return a, fmt.Errorf("delete requires at least one tag name, e.g. `tag delete v1.0.0`")
	}
	return a, nil
}

func parseShowArgs(a Args, rest []string) (Args, error) {
	var positional []string
	for _, arg := range rest {
		if strings.HasPrefix(arg, "-") {
			return a, fmt.Errorf("unknown flag for show: %s", arg)
		}
		positional = append(positional, arg)
	}
	if len(positional) != 1 {
		return a, fmt.Errorf("show requires exactly one tag name, e.g. `tag show v1.0.0`")
	}
	a.ShowName = positional[0]
	return a, nil
}

func parsePushArgs(a Args, rest []string) (Args, error) {
	var positional []string
	for i := 0; i < len(rest); i++ {
		arg := rest[i]
		switch {
		case arg == "--all":
			a.PushAll = true
		case arg == "-r" || arg == "--remote":
			v, err := nextVal(rest, i, arg)
			if err != nil {
				return a, err
			}
			a.PushRemote = v
			i++
		case strings.HasPrefix(arg, "--remote="):
			a.PushRemote = strings.TrimPrefix(arg, "--remote=")
		case strings.HasPrefix(arg, "-"):
			return a, fmt.Errorf("unknown flag for push: %s", arg)
		default:
			positional = append(positional, arg)
		}
	}
	if !a.PushAll && len(positional) == 0 {
		return a, fmt.Errorf("push requires a tag name (or --all), e.g. `tag push v1.0.0`")
	}
	a.PushTags = positional
	return a, nil
}

func parseVerifyArgs(a Args, rest []string) (Args, error) {
	for _, arg := range rest {
		if strings.HasPrefix(arg, "-") {
			return a, fmt.Errorf("unknown flag for verify: %s", arg)
		}
		a.TagNames = append(a.TagNames, arg)
	}
	if len(a.TagNames) == 0 {
		return a, fmt.Errorf("verify requires at least one tag name, e.g. `tag verify v1.0.0`")
	}
	return a, nil
}

func parseRenameArgs(a Args, rest []string) (Args, error) {
	var positional []string
	for i := 0; i < len(rest); i++ {
		arg := rest[i]
		if arg == "-f" || arg == "--force" {
			a.Force = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return a, fmt.Errorf("unknown flag for rename: %s", arg)
		}
		positional = append(positional, arg)
	}
	if len(positional) != 2 {
		return a, fmt.Errorf("rename requires exactly two arguments: OLD_NAME NEW_NAME")
	}
	a.OldName, a.NewName = positional[0], positional[1]
	return a, nil
}

func parseConfigArgs(a Args, rest []string) (Args, error) {
	if len(rest) == 0 {
		return a, fmt.Errorf("config requires a subcommand: `show` or `path`")
	}
	switch rest[0] {
	case "show", "path":
		a.ConfigSub = rest[0]
	default:
		return a, fmt.Errorf("unknown config subcommand: %s (expected `show` or `path`)", rest[0])
	}
	return a, nil
}

// ─── Git plumbing ───────────────────────────────────────────────────────────

type gitResult struct {
	Stdout string
	Stderr string
	Err    error
}

func runGit(dir string, args ...string) gitResult {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return gitResult{
		Stdout: strings.TrimRight(stdout.String(), "\n"),
		Stderr: strings.TrimRight(stderr.String(), "\n"),
		Err:    err,
	}
}

// runGitInteractive is used for commands (like -v / verify with a pager, or
// prompts) where we want git's own stdout/stderr passed straight through.
func runGitInteractive(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func gitAvailable() error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git executable not found in PATH — please install git")
	}
	return nil
}

// resolveDir expands ~, makes the path absolute, and confirms it exists.
func resolveDir(dir string) (string, error) {
	if strings.HasPrefix(dir, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("cannot resolve path %q: %w", dir, err)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("directory does not exist: %s", abs)
		}
		return "", fmt.Errorf("cannot access %s: %w", abs, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("not a directory: %s", abs)
	}
	return abs, nil
}

// gitRoot confirms dir is inside a git work tree and returns its top level.
func gitRoot(dir string) (string, error) {
	res := runGit(dir, "rev-parse", "--show-toplevel")
	if res.Err != nil {
		low := strings.ToLower(res.Stderr)
		if strings.Contains(low, "not a git repository") {
			return "", fmt.Errorf("not a git repository: %s\n%s", dir, muted("   (run this inside a repo, or point at one: `tag /path/to/repo ...`)"))
		}
		return "", fmt.Errorf("git error while checking repository: %s", res.Stderr)
	}
	return res.Stdout, nil
}

// isValidTagName delegates to `git check-ref-format`, the authoritative
// source for git's actual naming rules — no reimplementation, no guessing.
func isValidTagName(dir, name string) (bool, string) {
	if name == "" {
		return false, "tag name cannot be empty"
	}
	if name == "@" {
		return false, `tag name cannot be "@" (reserved)`
	}
	res := runGit(dir, "check-ref-format", "--allow-onelevel", "refs/tags/"+name)
	if res.Err != nil {
		return false, explainInvalidTagName(name)
	}
	return true, ""
}

// explainInvalidTagName gives a human-readable reason based on git's
// documented ref-naming rules (git-check-ref-format(1)), since git itself
// only reports a bare non-zero exit status.
func explainInvalidTagName(name string) string {
	switch {
	case strings.Contains(name, ".."):
		return `name cannot contain ".." `
	case strings.HasSuffix(name, "."):
		return "name cannot end with a dot (.)"
	case strings.HasSuffix(name, ".lock"):
		return `name cannot end with ".lock"`
	case strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.Contains(name, "//"):
		return "name cannot start/end with a slash, or contain consecutive slashes"
	case strings.Contains(name, "@{"):
		return `name cannot contain "@{"`
	case strings.ContainsAny(name, " ~^:?*[\\"):
		return `name cannot contain space, ~ ^ : ? * [ or \`
	case strings.HasPrefix(name, "-"):
		return "name should not start with a dash (-)"
	default:
		for _, part := range strings.Split(name, "/") {
			if strings.HasPrefix(part, ".") {
				return `no path component may start with a dot (.)`
			}
		}
		return "invalid tag name (see `git check-ref-format` rules)"
	}
}

func tagExists(dir, name string) bool {
	res := runGit(dir, "rev-parse", "--quiet", "--verify", "refs/tags/"+name)
	return res.Err == nil
}

func remoteExists(dir, remote string) bool {
	res := runGit(dir, "remote")
	if res.Err != nil {
		return false
	}
	for _, r := range strings.Split(res.Stdout, "\n") {
		if strings.TrimSpace(r) == remote {
			return true
		}
	}
	return false
}

// ─── List ───────────────────────────────────────────────────────────────────

type tagRow struct {
	Name    string
	Type    string // "tag" (annotated) or "commit" (lightweight)
	Hash    string
	Date    string
	Tagger  string
	Subject string
}

func listTags(dir string, cfg Config, a Args) error {
	format := strings.Join([]string{
		"%(refname:short)", "%(objecttype)", "%(objectname:short)",
		"%(creatordate:short)", "%(taggername)", "%(contents:subject)",
	}, fieldSep)

	sortKey := a.Sort
	if sortKey == "" {
		sortKey = cfg.DefaultSort
	}

	args := []string{"for-each-ref", "--format=" + format, "--sort=" + sortKey}
	if a.PointsAt != "" {
		args = append(args, "--points-at="+a.PointsAt)
	}
	if a.Contains != "" {
		args = append(args, "--contains="+a.Contains)
	}
	if a.NoContains != "" {
		args = append(args, "--no-contains="+a.NoContains)
	}
	if a.Merged != "" {
		args = append(args, "--merged="+a.Merged)
	}
	if a.NoMerged != "" {
		args = append(args, "--no-merged="+a.NoMerged)
	}
	args = append(args, "refs/tags/")

	res := runGit(dir, args...)
	if res.Err != nil {
		return fmt.Errorf("failed to list tags: %s", firstNonEmpty(res.Stderr, res.Err.Error()))
	}
	if strings.TrimSpace(res.Stdout) == "" {
		fmt.Println(muted("(no tags in this repository)"))
		return nil
	}

	var rows []tagRow
	for _, line := range strings.Split(res.Stdout, "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, fieldSep)
		for len(f) < 6 {
			f = append(f, "")
		}
		if a.Pattern != "" {
			match, _ := filepath.Match(a.Pattern, f[0])
			if !match && !strings.Contains(f[0], a.Pattern) {
				continue
			}
		}
		rows = append(rows, tagRow{Name: f[0], Type: f[1], Hash: f[2], Date: f[3], Tagger: f[4], Subject: f[5]})
	}

	if len(rows) == 0 {
		fmt.Println(muted("(no tags match)"))
		return nil
	}

	nameWidth := 0
	for _, r := range rows {
		if len(r.Name) > nameWidth {
			nameWidth = len(r.Name)
		}
	}

	for _, r := range rows {
		kind := color(cfg.Lightweight, "◦ lightweight")
		if r.Type == "tag" {
			kind = color(cfg.Annotated, "● annotated")
		}
		name := color(cfg.TagName, padRight(r.Name, nameWidth))

		if !a.ShowAll {
			line := fmt.Sprintf("%s  %s", name, kind)
			if a.NLines != 0 && r.Subject != "" {
				line += "  " + color(cfg.Message, r.Subject)
			}
			fmt.Println(line)
			continue
		}

		fmt.Printf("%s  %s  %s  %s\n", name, kind, color(cfg.Hash, r.Hash), color(cfg.Date, r.Date))
		if r.Tagger != "" {
			fmt.Println("   " + color(cfg.Tagger, "👤 "+r.Tagger))
		}
		if r.Subject != "" && a.NLines != 0 {
			fmt.Println("   " + color(cfg.Message, "📝 "+r.Subject))
		}
	}
	fmt.Println()
	fmt.Printf("%s %d tag(s)\n", muted("total:"), len(rows))
	return nil
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ─── Add ────────────────────────────────────────────────────────────────────

func addTag(dir string, cfg Config, a Args) error {
	if valid, reason := isValidTagName(dir, a.NewTagName); !valid {
		return fmt.Errorf("invalid tag name %q: %s", a.NewTagName, reason)
	}

	if tagExists(dir, a.NewTagName) && !a.Force {
		existing := runGit(dir, "rev-parse", "--short", "refs/tags/"+a.NewTagName).Stdout
		return fmt.Errorf(
			"tag %q already exists (points at %s)\n%s",
			a.NewTagName, existing,
			muted("   use --force to move it, or `tag delete "+a.NewTagName+"` first"),
		)
	}

	// Confirm the target commit/object actually resolves before we let git
	// fail with a more cryptic message.
	if res := runGit(dir, "rev-parse", "--verify", "--quiet", a.At); res.Err != nil {
		return fmt.Errorf("target %q does not resolve to a valid commit/object in this repository", a.At)
	}

	args := []string{"tag"}
	switch {
	case a.Signed:
		args = append(args, "-s")
	case a.KeyID != "":
		args = append(args, "-u", a.KeyID)
	case a.Annotated:
		args = append(args, "-a")
	}
	if a.Force {
		args = append(args, "-f")
	}
	if a.Message != "" {
		args = append(args, "-m", a.Message)
	} else if a.MessageFile != "" {
		args = append(args, "-F", a.MessageFile)
	} else if a.Annotated {
		return fmt.Errorf("annotated tags need a message: pass -m \"...\" or -F <file>")
	}
	args = append(args, a.NewTagName, a.At)

	res := runGit(dir, args...)
	if res.Err != nil {
		return fmt.Errorf("git tag failed: %s", firstNonEmpty(res.Stderr, res.Err.Error()))
	}

	kind := "lightweight"
	if a.Annotated {
		kind = "annotated"
	}
	short := runGit(dir, "rev-parse", "--short", a.NewTagName).Stdout
	fmt.Printf("%s created %s tag %s at %s\n",
		ok("✔"), kind, color(cfg.TagName, a.NewTagName), color(cfg.Hash, short))
	return nil
}

// ─── Delete ─────────────────────────────────────────────────────────────────

func deleteTags(dir string, cfg Config, a Args) error {
	var missing, deleted []string
	for _, name := range a.TagNames {
		if !tagExists(dir, name) {
			missing = append(missing, name)
			continue
		}
		res := runGit(dir, "tag", "-d", name)
		if res.Err != nil {
			return fmt.Errorf("failed to delete %q: %s", name, firstNonEmpty(res.Stderr, res.Err.Error()))
		}
		deleted = append(deleted, name)
	}
	for _, name := range deleted {
		fmt.Printf("%s deleted tag %s\n", ok("✔"), color(cfg.TagName, name))
	}
	for _, name := range missing {
		fmt.Printf("%s tag %q does not exist locally — skipped\n", warn("⚠"), name)
	}
	if len(deleted) == 0 {
		return fmt.Errorf("no tags were deleted")
	}
	fmt.Println(muted("   note: this only removes the local tag(s). If already pushed, also delete on the remote:"))
	for _, name := range deleted {
		fmt.Println(muted(fmt.Sprintf("     git push <remote> --delete %s", name)))
	}
	return nil
}

// ─── Show ───────────────────────────────────────────────────────────────────

func showTag(dir string, cfg Config, a Args) error {
	if !tagExists(dir, a.ShowName) {
		return fmt.Errorf("tag %q does not exist", a.ShowName)
	}
	objType := runGit(dir, "cat-file", "-t", "refs/tags/"+a.ShowName).Stdout
	fmt.Printf("%s %s\n", color(cfg.Header, "🏷  Tag:"), color(cfg.TagName, a.ShowName))

	if objType == "tag" {
		fmt.Println(color(cfg.Annotated, "   type: annotated"))
		info := runGit(dir, "for-each-ref",
			"--format=%(taggername)"+fieldSep+"%(taggeremail)"+fieldSep+"%(creatordate)"+fieldSep+"%(*objectname:short)",
			"refs/tags/"+a.ShowName).Stdout
		f := strings.Split(info, fieldSep)
		for len(f) < 4 {
			f = append(f, "")
		}
		if f[0] != "" {
			fmt.Printf("   %s %s %s\n", color(cfg.Tagger, "tagger:"), f[0], muted(f[1]))
		}
		if f[2] != "" {
			fmt.Printf("   %s %s\n", color(cfg.Date, "date:"), f[2])
		}
		if f[3] != "" {
			fmt.Printf("   %s %s\n", color(cfg.Hash, "commit:"), f[3])
		}
		msg := runGit(dir, "for-each-ref", "--format=%(contents)", "refs/tags/"+a.ShowName).Stdout
		if strings.TrimSpace(msg) != "" {
			fmt.Println()
			fmt.Println(color(cfg.Message, strings.TrimRight(msg, "\n")))
		}
	} else {
		fmt.Println(color(cfg.Lightweight, "   type: lightweight"))
		short := runGit(dir, "rev-parse", "--short", "refs/tags/"+a.ShowName).Stdout
		fmt.Printf("   %s %s\n", color(cfg.Hash, "commit:"), short)
		subject := runGit(dir, "log", "-1", "--format=%s", "refs/tags/"+a.ShowName).Stdout
		date := runGit(dir, "log", "-1", "--format=%ad", "--date=short", "refs/tags/"+a.ShowName).Stdout
		if date != "" {
			fmt.Printf("   %s %s\n", color(cfg.Date, "date:"), date)
		}
		if subject != "" {
			fmt.Println()
			fmt.Println(color(cfg.Message, subject))
		}
	}
	return nil
}

// ─── Push ───────────────────────────────────────────────────────────────────

func pushTags(dir string, cfg Config, a Args) error {
	remote := a.PushRemote
	if remote == "" {
		remote = cfg.DefaultRemote
	}
	if !remoteExists(dir, remote) {
		remotes := runGit(dir, "remote").Stdout
		hint := "(no remotes configured)"
		if remotes != "" {
			hint = "available: " + strings.ReplaceAll(remotes, "\n", ", ")
		}
		return fmt.Errorf("remote %q not found — %s", remote, hint)
	}

	var args []string
	var label string
	if a.PushAll {
		args = []string{"push", remote, "--tags"}
		label = "all tags"
	} else {
		for _, name := range a.PushTags {
			if !tagExists(dir, name) {
				return fmt.Errorf("tag %q does not exist locally — nothing to push", name)
			}
		}
		args = append([]string{"push", remote}, a.PushTags...)
		label = strings.Join(a.PushTags, ", ")
	}

	fmt.Printf("%s pushing %s to %s...\n", muted("→"), label, color(cfg.Remote, remote))
	if err := runGitInteractive(dir, args...); err != nil {
		return fmt.Errorf("push failed (see git output above)")
	}
	fmt.Printf("%s pushed %s to %s\n", ok("✔"), label, color(cfg.Remote, remote))
	return nil
}

// ─── Verify ─────────────────────────────────────────────────────────────────

func verifyTags(dir string, cfg Config, a Args) error {
	failures := 0
	for _, name := range a.TagNames {
		if !tagExists(dir, name) {
			fmt.Printf("%s %s — does not exist\n", fail("✘"), name)
			failures++
			continue
		}
		res := runGit(dir, "tag", "-v", name)
		if res.Err != nil {
			fmt.Printf("%s %s — %s\n", fail("✘"), color(cfg.TagName, name), firstNonEmpty(lastLine(res.Stderr), "signature verification failed"))
			failures++
			continue
		}
		fmt.Printf("%s %s — signature valid\n", ok("✔"), color(cfg.TagName, name))
	}
	if failures > 0 {
		return fmt.Errorf("%d/%d tag(s) failed verification", failures, len(a.TagNames))
	}
	return nil
}

func lastLine(s string) string {
	parts := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return parts[len(parts)-1]
}

// ─── Rename ─────────────────────────────────────────────────────────────────

// renameTag re-creates OLD as NEW (preserving annotation/message/target) and
// deletes OLD. Git has no native tag-rename: an annotated tag is an
// immutable object, so this is the closest correct equivalent. If OLD was
// already pushed, the caller still needs to push NEW and delete OLD remotely.
func renameTag(dir string, cfg Config, a Args) error {
	if !tagExists(dir, a.OldName) {
		return fmt.Errorf("tag %q does not exist", a.OldName)
	}
	if valid, reason := isValidTagName(dir, a.NewName); !valid {
		return fmt.Errorf("invalid new tag name %q: %s", a.NewName, reason)
	}
	if tagExists(dir, a.NewName) && !a.Force {
		return fmt.Errorf("target name %q already exists — use --force to overwrite", a.NewName)
	}

	objType := runGit(dir, "cat-file", "-t", "refs/tags/"+a.OldName).Stdout
	target := runGit(dir, "rev-list", "-n", "1", a.OldName).Stdout

	var createArgs []string
	if objType == "tag" {
		msg := runGit(dir, "for-each-ref", "--format=%(contents)", "refs/tags/"+a.OldName).Stdout
		msg = strings.TrimRight(msg, "\n")
		createArgs = []string{"tag", "-a", a.NewName, "-m", msg}
	} else {
		createArgs = []string{"tag", a.NewName}
	}
	if a.Force {
		createArgs = append(createArgs, "-f")
	}
	createArgs = append(createArgs, target)

	if res := runGit(dir, createArgs...); res.Err != nil {
		return fmt.Errorf("failed to create %q: %s", a.NewName, firstNonEmpty(res.Stderr, res.Err.Error()))
	}
	if res := runGit(dir, "tag", "-d", a.OldName); res.Err != nil {
		// Roll back the new tag so we don't leave two tags behind on failure.
		runGit(dir, "tag", "-d", a.NewName)
		return fmt.Errorf("created %q but failed to delete %q: %s — rolled back", a.NewName, a.OldName, firstNonEmpty(res.Stderr, res.Err.Error()))
	}

	fmt.Printf("%s renamed %s → %s\n", ok("✔"), color(cfg.TagName, a.OldName), color(cfg.TagName, a.NewName))
	fmt.Println(muted("   note: this created a new tag object; if the old tag was pushed, push the new one"))
	fmt.Println(muted("   and delete the old one on the remote: git push <remote> :refs/tags/" + a.OldName))
	return nil
}

// ─── Config command ─────────────────────────────────────────────────────────

func showConfigCmd(a Args, cfg Config) error {
	if a.ConfigSub == "path" {
		fmt.Println(color("#4ECDC4", "🔎 Configuration search paths (ordered by priority):"))
		for _, path := range configCandidates() {
			if _, err := os.Stat(path); err == nil {
				fmt.Printf("  %s %s\n", ok("✔ [ACTIVE]"), path)
			} else {
				fmt.Printf("             %s\n", muted(path))
			}
		}
		return nil
	}
	pretty, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("error formatting configuration: %w", err)
	}
	fmt.Println(color("#FFAAFF", "📝 Active Configuration State:"))
	fmt.Println(string(pretty))
	return nil
}

// ─── Help ───────────────────────────────────────────────────────────────────

func printVersion() { fmt.Printf("tag v%s\n", Version) }

func printHelp() {
	cfg := defaultConfig()
	fmt.Print(`
` + color(cfg.Header, "🏷  tag v"+Version) + ` — Colorized, safety-checked git tag management

` + color("#FFE66D", "USAGE:") + `
  tag [WORKING_DIR] [COMMAND] [ARGS] [FLAGS]

  WORKING_DIR is optional and defaults to the current directory — you never
  need to pass "." explicitly. It's recognized when it's path-like (starts
  with . / ~) or already exists as a directory; otherwise use -C/--dir.

` + color("#4ECDC4", "COMMANDS:") + `
  ` + color(cfg.TagName, "(none) | list | ls") + `      List tags ` + muted("[default]") + `
      [PATTERN] [-a|--all] [-n[NUM]] [--sort=KEY] [--points-at=OBJ]
      [--contains=COMMIT] [--no-contains=COMMIT] [--merged=COMMIT] [--no-merged=COMMIT]

  ` + color(cfg.TagName, "add | create | new") + ` NAME [TARGET]   Create a tag
      [-a|--annotate] [-s|--sign] [-u KEYID] [-m MSG] [-F FILE] [-f|--force]
      TARGET defaults to HEAD.

  ` + color(cfg.TagName, "delete | rm | remove") + ` NAME...   Delete one or more local tags

  ` + color(cfg.TagName, "show | info") + ` NAME                  Show tag details (type, tagger, date, message)

  ` + color(cfg.TagName, "push | publish") + ` NAME... [--all] [-r REMOTE]   Push tag(s) to a remote (default: origin)

  ` + color(cfg.TagName, "verify") + ` NAME...                    Verify GPG signature(s) on annotated tag(s)

  ` + color(cfg.TagName, "rename | mv") + ` OLD NEW [-f]           Recreate OLD as NEW, then delete OLD

  ` + color(cfg.TagName, "config show|path") + `                  Show active config, or config file search paths

` + color("#F38181", "EXAMPLES:") + `
  ` + color(cfg.TagName, "tag") + `                              List tags in the current directory
  ` + color(cfg.TagName, "tag ~/projects/app") + `               List tags in another repo (positional dir)
  ` + color(cfg.TagName, "tag -C ~/projects/app add v1") + `      Same, but using -C (unambiguous)
  ` + color(cfg.TagName, `tag add v1.2.0 -a -m "Release 1.2.0"`) + `
  ` + color(cfg.TagName, "tag add hotfix -f") + `                 Move an existing lightweight tag (force)
  ` + color(cfg.TagName, "tag delete v0.9.0-beta") + `
  ` + color(cfg.TagName, "tag show v1.2.0") + `
  ` + color(cfg.TagName, "tag push v1.2.0") + `                   Push a single tag to origin
  ` + color(cfg.TagName, "tag push --all -r upstream") + `        Push all tags to a specific remote
  ` + color(cfg.TagName, "tag verify v1.2.0") + `
  ` + color(cfg.TagName, "tag rename v1.2.0 v1.2.0-rc1") + `

` + color("#FEDE5D", "CONFIGURATION:") + `
  Supported filenames: tag.json, .tag.json
  Lookup order: $TAG_CONFIG → exe dir → cwd → platform config dir → home dir
  Colors are 24-bit hex (#RRGGBB or #RGB). Set NO_COLOR=1 or TAG_NO_COLOR=1 to disable color.

` + color("#FEDE5D", "ENVIRONMENT:") + `
  TAG_CONFIG       Path to a custom config file
  NO_COLOR / TAG_NO_COLOR   Disable colored output

`)
}

// ─── Main ───────────────────────────────────────────────────────────────────

func main() {
	if err := gitAvailable(); err != nil {
		fmt.Fprintln(os.Stderr, fail("❌ "+err.Error()))
		os.Exit(1)
	}

	a, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, fail("❌ "+err.Error()))
		fmt.Fprintln(os.Stderr, muted("   run `tag --help` for usage"))
		os.Exit(2)
	}

	if a.Command == CmdHelp {
		printHelp()
		return
	}
	if a.Command == CmdVersion {
		printVersion()
		return
	}

	cfg := loadConfig()

	if a.Command == CmdConfig {
		if err := showConfigCmd(a, cfg); err != nil {
			fmt.Fprintln(os.Stderr, fail("❌ "+err.Error()))
			os.Exit(1)
		}
		return
	}

	dir, err := resolveDir(a.Dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, fail("❌ "+err.Error()))
		os.Exit(1)
	}

	root, err := gitRoot(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, fail("❌ ")+err.Error())
		os.Exit(1)
	}

	var runErr error
	switch a.Command {
	case CmdList:
		runErr = listTags(root, cfg, a)
	case CmdAdd:
		runErr = addTag(root, cfg, a)
	case CmdDelete:
		runErr = deleteTags(root, cfg, a)
	case CmdShow:
		runErr = showTag(root, cfg, a)
	case CmdPush:
		runErr = pushTags(root, cfg, a)
	case CmdVerify:
		runErr = verifyTags(root, cfg, a)
	case CmdRename:
		runErr = renameTag(root, cfg, a)
	}

	if runErr != nil {
		fmt.Fprintln(os.Stderr, fail("❌ ")+runErr.Error())
		os.Exit(1)
	}
}
