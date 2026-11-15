package supervisor

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDockerRunNotPrivileged(t *testing.T) {
	// A pack container with this flag is host root outside the wall.
	// Launch argv is a slice built in the same function, so that form counts too.
	if !sourceHasPrivilegedRun("package p\nfunc f() {\n\tDocker(time.Minute, \"run\", \"--rm\", \"--privileged\")\n}\n") {
		t.Fatal("detector missed a privileged Docker run")
	}
	if !sourceHasPrivilegedRun("package p\nfunc f() {\n\targs := []string{\"run\", \"--rm\"}\n\targs = append(args, \"--privileged\")\n\tDocker(time.Minute, args...)\n}\n") {
		t.Fatal("detector missed a privileged Docker run slice")
	}
	if sourceHasPrivilegedRun("package p\nfunc f() {\n\targs := []string{\"run\", \"--rm\", \"--cap-add\", \"NET_ADMIN\"}\n\tDocker(time.Minute, args...)\n}\n") {
		t.Fatal("detector flagged an unprivileged Docker run")
	}

	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var hits []string
	err = filepath.WalkDir(filepath.Join(root, "supervisor"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, pos := range privilegedDockerRuns(f) {
			p := fset.Position(pos)
			hits = append(hits, p.Filename+":"+strconv.Itoa(p.Line))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) > 0 {
		t.Fatalf("privileged Docker run:\n%s", strings.Join(hits, "\n"))
	}
}

func sourceHasPrivilegedRun(src string) bool {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "snippet.go", src, 0)
	if err != nil {
		return false
	}
	return len(privilegedDockerRuns(f)) > 0
}

func privilegedDockerRuns(f *ast.File) []token.Pos {
	var hits []token.Pos
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return true
		}
		hits = append(hits, privilegedRunsIn(fn.Body)...)
		return false
	})
	return hits
}

func privilegedRunsIn(body *ast.BlockStmt) []token.Pos {
	argv := map[string][]ast.Expr{}
	var hits []token.Pos
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			trackArgv(argv, n)
		case *ast.CallExpr:
			if isDockerCall(n) && runPassesPrivileged(n, argv) {
				hits = append(hits, n.Pos())
			}
		}
		return true
	})
	return hits
}

func trackArgv(argv map[string][]ast.Expr, as *ast.AssignStmt) {
	if len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return
	}
	id, ok := as.Lhs[0].(*ast.Ident)
	if !ok || id.Name == "_" {
		return
	}
	argv[id.Name] = flattenArgv(as.Rhs[0], argv)
}

func flattenArgv(e ast.Expr, argv map[string][]ast.Expr) []ast.Expr {
	switch e := e.(type) {
	case *ast.Ident:
		if el, ok := argv[e.Name]; ok {
			return el
		}
	case *ast.CompositeLit:
		return e.Elts
	case *ast.CallExpr:
		id, ok := e.Fun.(*ast.Ident)
		if ok && id.Name == "append" && len(e.Args) > 0 {
			out := append([]ast.Expr{}, flattenArgv(e.Args[0], argv)...)
			return append(out, e.Args[1:]...)
		}
	}
	return []ast.Expr{e}
}

func isDockerCall(call *ast.CallExpr) bool {
	id, ok := call.Fun.(*ast.Ident)
	return ok && (id.Name == "Docker" || id.Name == "DockerIgnore")
}

func runPassesPrivileged(call *ast.CallExpr, argv map[string][]ast.Expr) bool {
	args := call.Args
	if call.Ellipsis.IsValid() && len(args) > 0 {
		head := append([]ast.Expr{}, args[:len(args)-1]...)
		args = append(head, flattenArgv(args[len(args)-1], argv)...)
	}
	run, priv := false, false
	for _, arg := range args {
		s, ok := constString(arg)
		if !ok {
			continue
		}
		if s == "run" {
			run = true
		}
		if s == "--privileged" || strings.HasPrefix(s, "--privileged=") {
			priv = true
		}
	}
	return run && priv
}

func constString(e ast.Expr) (string, bool) {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return constString(e.X)
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}
		return v, true
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		l, lok := constString(e.X)
		r, rok := constString(e.Y)
		if lok && rok {
			return l + r, true
		}
	}
	return "", false
}
