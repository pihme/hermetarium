package supervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func Run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	root, err := Root()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cmd := args[0]
	rest := args[1:]
	switch cmd {
	case "create":
		wall := flag(rest, "--wall", "weak")
		example := flag(rest, "--example", ExampleEcho)
		if _, ok := LookupExample(example); !ok {
			fmt.Fprintf(os.Stderr, "unknown example %q\n", example)
			return 2
		}
		id, err := NewID()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		switch wall {
		case "strong":
			_, err = CreateStrongExample(root, id, example)
		case "weak":
			_, err = CreateWeakExample(root, id, example)
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

func usage() {
	fmt.Fprintf(os.Stderr, `usage: hermetarium create --wall weak|strong [--example echo|claude-code|grok-build|deepseek-harness]
       hermetarium url <id>
       hermetarium exec <id> -- <cmd>
       hermetarium logs <id>
       hermetarium destroy <id>
probe host: %s
`, ProbeHost)
}
