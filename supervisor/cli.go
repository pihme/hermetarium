package supervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func Run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	cmd := args[0]
	rest := args[1:]
	if cmd == "version" {
		fmt.Println(Version)
		return 0
	}
	root, err := Root()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	switch cmd {
	case "create":
		wall := flag(rest, "--wall", "weak")
		ex, err := ResolveCreate(rest)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		if wall != "strong" && (flagPresent(rest, "--mem") || flagPresent(rest, "--disk")) {
			fmt.Fprintln(os.Stderr, "--mem and --disk apply only to --wall strong")
			return 2
		}
		id, err := NewID()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		switch wall {
		case "strong":
			_, err = CreateStrong(root, id, ex)
		case "weak":
			_, err = CreateWeak(root, id, ex)
		default:
			fmt.Fprintf(os.Stderr, "unknown wall %q (want weak or strong)\n", wall)
			return 2
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println(id)
		return 0
	case "exec":
		if len(rest) < 1 {
			usage()
			return 2
		}
		id := rest[0]
		argv := rest[1:]
		if len(argv) > 0 && argv[0] == "--" {
			argv = argv[1:]
		}
		if len(argv) == 0 {
			argv = []string{"sh"}
		}
		inst, err := LoadWeak(root, id)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if inst.Wall != "weak" {
			fmt.Fprintf(os.Stderr, "exec is only implemented for the weak wall (instance %s is %s)\n", id, inst.Wall)
			return 1
		}
		stdout, stderr, code := ExecWeak(inst, argv)
		os.Stdout.WriteString(stdout)
		os.Stderr.WriteString(stderr)
		return code
	case "url":
		if len(rest) < 1 {
			usage()
			return 2
		}
		u, err := LoadInboundURL(root, rest[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println(u)
		return 0
	case "logs":
		if len(rest) < 1 {
			usage()
			return 2
		}
		id := rest[0]
		dir := filepath.Join(VarDir(root), id)
		if err := SyncInstanceLog(dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		recs, err := ReadIoLog(filepath.Join(dir, "io.jsonl"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		enc := json.NewEncoder(os.Stdout)
		for _, r := range recs {
			_ = enc.Encode(r)
		}
		return 0
	case "destroy":
		if len(rest) < 1 {
			usage()
			return 2
		}
		id := rest[0]
		_ = DestroyWeak(id)
		_ = DestroyStrong(id)
		return 0
	default:
		usage()
		return 2
	}
}

func flag(args []string, name, fallback string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return fallback
}

func flagPresent(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

// sizeFlag reads a positive integer. A missing flag returns fallback.
func sizeFlag(args []string, name string, fallback int) (int, error) {
	if !flagPresent(args, name) {
		return fallback, nil
	}
	raw := strings.TrimSpace(flag(args, name, ""))
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s requires a positive integer", name)
	}
	return n, nil
}

// ResolveCreate requires --image and --acl.
// --mem and --disk are optional; defaults are 512 MiB and 1024 MiB.
// The caller rejects those flags unless --wall strong.
func ResolveCreate(args []string) (CreateOpts, error) {
	image := strings.TrimSpace(flag(args, "--image", ""))
	if image == "" {
		return CreateOpts{}, fmt.Errorf("create requires --image NAME")
	}
	acl := strings.TrimSpace(flag(args, "--acl", ""))
	if acl == "" {
		return CreateOpts{}, fmt.Errorf("create requires --acl FILE")
	}
	mem, err := sizeFlag(args, "--mem", 512)
	if err != nil {
		return CreateOpts{}, err
	}
	disk, err := sizeFlag(args, "--disk", 1024)
	if err != nil {
		return CreateOpts{}, err
	}
	return CreateOpts{Image: image, ACL: acl, MemMiB: mem, DiskMB: disk}, nil
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage: hermetarium create --wall weak|strong --image NAME --acl FILE
       hermetarium create --wall strong --image NAME --acl FILE [--mem MiB] [--disk MiB]
       hermetarium url <id>
       hermetarium exec <id> -- <cmd>
       hermetarium logs <id>
       hermetarium destroy <id>
       hermetarium version
`)
}
