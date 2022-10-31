package cfgutil

import (
	"bytes"
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
func ParseTOML(r io.Reader, dst any) error {
	return toml.NewDecoder(r).Decode(dst)
}

// ParseJSON parses a JSON config from an io.Reader.
func ParseJSON(r io.Reader, dst any) error {
	return json.NewDecoder(r).Decode(dst)
}

// Parse parses a reader.
func Parse(f io.Reader, configType string, dst any) error {
	switch configType {
	case "toml":
		return ParseTOML(f, dst)
	case "json":
		return ParseJSON(f, dst)
	default:
		return fmt.Errorf("unsupported config type %s", configType)
	}
}

// ParseMany parses b into multiple destinations.
func ParseMany(b []byte, configType string, dsts ...any) error {
	for _, dst := range dsts {
		if err := Parse(bytes.NewReader(b), configType, dst); err != nil {
			return fmt.Errorf("cannot parse into %T: %w", dst, err)
		}
	}
	return nil
}

// ParseFile parses a config file from a path. The file extension is used to
// determine the config format.
func ParseFile[T any](path string) (*T, error) {
	ext := filepath.Ext(path)

	f, err := os.Open(path)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open config file")
	}
	defer f.Close()

	var v T
	err = Parse(f, strings.TrimPrefix(ext, "."), &v)
	return &v, err
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
