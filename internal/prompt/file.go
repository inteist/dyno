package prompt

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// fileSet is the JSON form of a prompt file.
type fileSet struct {
	Name     string `json:"name"`
	System   string `json:"system"`
	Question string `json:"question"`
	FollowUp string `json:"follow_up"`
}

// Resolve returns the built-in set with that name, or loads a JSON file.
func Resolve(nameOrPath string) (Set, error) {
	if set, ok := Builtin(nameOrPath); ok {
		return set, nil
	}
	set, err := LoadFile(nameOrPath)
	if errors.Is(err, fs.ErrNotExist) {
		return Set{}, fmt.Errorf("unknown prompt %q: use %s, or the path of a JSON file",
			nameOrPath, strings.Join(BuiltinNames(), ", "))
	}
	return set, err
}

// LoadFile reads a prompt file with the keys name, system, question, and
// follow_up. Without a name, the set takes the file name without extension.
// The name must not be the name of a built-in set, because the report
// filters by name.
func LoadFile(path string) (Set, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Set{}, fmt.Errorf("read prompt file: %w", err)
	}
	f, err := decodeFile(data)
	if err != nil {
		return Set{}, fmt.Errorf("parse prompt file %s: %w", path, err)
	}
	if f.Name == "" {
		f.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if err := checkFile(f); err != nil {
		return Set{}, fmt.Errorf("prompt file %s: %w", path, err)
	}
	return Set(f), nil
}

// decodeFile reads exactly one JSON object with the known keys.
func decodeFile(data []byte) (fileSet, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f fileSet
	if err := dec.Decode(&f); err != nil {
		return fileSet{}, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fileSet{}, errors.New("the file has more data after the JSON object")
	}
	return f, nil
}

// checkFile returns the first problem of the content of a prompt file.
func checkFile(f fileSet) error {
	if strings.TrimSpace(f.Question) == "" {
		return errors.New("the key question is required")
	}
	if slices.Contains(BuiltinNames(), f.Name) {
		return fmt.Errorf("the name %q belongs to a built-in prompt", f.Name)
	}
	return nil
}
