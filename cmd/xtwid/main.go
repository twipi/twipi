package main

import (
	"bytes"
	"flag"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"

	"github.com/pkg/errors"
)

var (
	twikitVersion      = "latest"
	twikitModule       = "github.com/twipi/twikit"
	output             = "twid"
	debug              = false
	extraModules       StringsFlag
	extraImports       StringsFlag
	extraImportModules StringsFlag
	extraReplaces      StringsFlag
)

func init() {
	flag.StringVar(&twikitVersion, "twikit-version", twikitVersion, "twikit version")
	flag.StringVar(&twikitModule, "twikit-module", twikitModule, "twikit module")
	flag.StringVar(&output, "output", output, "output binary name")
	flag.BoolVar(&debug, "debug", debug, "do not delete tmp directory")
	flag.Var(&extraModules, "module", "extra Go modules to include in the generated code")
	flag.Var(&extraImports, "import", "extra Go imports to include in the generated code")
	flag.Var(&extraReplaces, "replace", "extra Go replaces to include in the generated code")
	flag.Var(&extraImportModules, "import-module", "extra Go modules (with a root import) to include in the generated code")
}

func main() {
	flag.Parse()

	if err := build(); err != nil {
		log.Fatalln(err)
	}
}

func build() error {
	dir, err := os.MkdirTemp("", "xtwid-*")
	if err != nil {
		return errors.Wrap(err, "failed to create temporary directory")
	}

	if !debug {
		defer os.RemoveAll(dir)
	}

	steps := []func(*execer) error{
		buildGoMod,
		generateMain,
		tidyGoMod,
		buildMain,
	}

	execer := execer{pwd: dir}
	for _, step := range steps {
		if err := step(&execer); err != nil {
			return err
		}
	}

	return nil
}

func generateMain(execer *execer) error {
	var b bytes.Buffer
	b.WriteString("package main\n\n")
	b.WriteString("import (\n")
	b.WriteString(strconv.Quote(path.Join(twikitModule, "twid")))
	b.WriteString("\n")
	for _, import_ := range slices(extraImports, extraImportModules) {
		b.WriteString("\t_ ")
		b.WriteString(strconv.Quote(import_))
		b.WriteString("\n")
	}
	b.WriteString(")\n\n")
	b.WriteString("func main() {\n")
	b.WriteString("\ttwid.Main()\n")
	b.WriteString("}\n")

	if err := os.WriteFile(filepath.Join(execer.pwd, "main.go"), b.Bytes(), 0644); err != nil {
		return errors.Wrap(err, "failed to write main.go")
	}

	return nil
}

func buildMain(execer *execer) error {
	dst := output
	if !filepath.IsAbs(dst) {
		pwd, err := os.Getwd()
		if err != nil {
			return errors.Wrap(err, "failed to get current working directory")
		}

		dst = filepath.Join(pwd, dst)
	}

	if stat, err := os.Stat(dst); err == nil && stat.IsDir() {
		return errors.New("output path is a directory (try removing it or setting -o)")
	}

	if err := execer.exec("go", "build", "-o", dst); err != nil {
		return errors.Wrapf(err, "failed to build twid to %q", dst)
	}

	return nil
}

func buildGoMod(execer *execer) error {
	modName := "localhost" + filepath.ToSlash(execer.pwd)

	if err := execer.exec("go", "mod", "init", modName); err != nil {
		return errors.Wrap(err, "failed to initialize Go module")
	}

	modules := slices(
		[]string{twikitModule + "@" + twikitVersion},
		extraModules,
		extraImportModules,
	)

	for _, replace := range extraReplaces {
		if err := execer.exec("go", "mod", "edit", "-replace="+replace); err != nil {
			return errors.Wrapf(err, "failed to replace module %q", replace)
		}
	}

	for _, module := range modules {
		if err := execer.exec("go", "get", module); err != nil {
			return errors.Wrapf(err, "failed to get module %q", module)
		}
	}

	return nil
}

func tidyGoMod(execer *execer) error {
	if err := execer.exec("go", "mod", "tidy"); err != nil {
		return errors.Wrap(err, "failed to tidy Go module")
	}

	return nil
}

type execer struct {
	pwd string
}

func (e *execer) exec(arg0 string, argv ...string) error {
	cmd := exec.Command(arg0, argv...)
	cmd.Dir = e.pwd
	if debug {
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

func slices[T any](slices ...[]T) []T {
	var total int
	for _, slice := range slices {
		total += len(slice)
	}

	out := make([]T, 0, total)
	for _, slice := range slices {
		out = append(out, slice...)
	}

	return out
}
