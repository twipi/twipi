package cfgutil

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"
	"github.com/pkg/errors"
)

// ParseTOML parses a TOML config from an io.Reader.
func ParseTOML[T any](r io.Reader) (*T, error) {
	var c T
	if err := toml.NewDecoder(r).Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// ParseJSON parses a JSON config from an io.Reader.
func ParseJSON[T any](r io.Reader) (*T, error) {
	var c T
	if err := json.NewDecoder(r).Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// ParseFile parses a config file from a path. The file extension is used to
// determine the config format.
func ParseFile[T any](path string) (*T, error) {
	ext := filepath.Ext(path)

	switch ext {
	case ".toml", ".json":
		f, err := os.Open(path)
		if err != nil {
			return nil, errors.Wrap(err, "failed to open config file")
		}
		defer f.Close()

		switch filepath.Ext(path) {
		case ".toml":
			return ParseTOML[T](f)
		case ".json":
			return ParseJSON[T](f)
		}
	}

	return nil, fmt.Errorf("unsupported config file extension %s", ext)
}

// Env is a type that describes a value that can also be an environment
// variable if the value is of format $ENV.
type Env[T ~string] string

var envCache sync.Map

func (env Env[T]) String() string {
	return string(env.Value())
}

func (env Env[T]) Value() T {
	if strings.HasPrefix(string(env), "$") {
		if v, ok := envCache.Load(string(env)); ok {
			return T(v.(string))
		}
		v := os.ExpandEnv(string(env))
		envCache.Store(string(env), v)
		return T(v)
	}

	return T(string(env))
}

// EnvString is a string variant of Env.
type EnvString = Env[string]
