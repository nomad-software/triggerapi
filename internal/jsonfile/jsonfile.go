// Package jsonfile reads and writes values as indented JSON files, which can
// be encrypted.
package jsonfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/nomad-software/triggerapi/internal/crypt"
)

// Load reads the JSON file at path into v. A missing file is not an error and
// leaves v untouched.
func Load(path string, v any) error {
	return LoadWith(path, v, nil)
}

// LoadWith reads the JSON file at path into v, decrypting it with keys if it
// is encrypted. A missing file is not an error and leaves v untouched.
func LoadWith(path string, v any, keys *crypt.Keys) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	data, err = keys.Decrypt(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// Save writes v to the JSON file at path, replacing it atomically.
func Save(path string, v any) error {
	return SaveWith(path, v, nil)
}

// SaveWith writes v to the JSON file at path, encrypted with keys unless
// they are nil, replacing it atomically.
func SaveWith(path string, v any, keys *crypt.Keys) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data, err = keys.Encrypt(append(data, '\n'))
	if err != nil {
		return err
	}
	return write(path, data)
}

// Convert has the file at path, which was written with the keys from,
// written with the keys to: encrypted, decrypted, or encrypted with other
// keys. Nil keys are for a file that is not encrypted. A file that is
// missing, or already as it is to be, is left alone.
func Convert(path string, from, to *crypt.Keys) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	plain := data
	if crypt.Encrypted(data) {
		if from == to {
			return nil
		}
		plain, err = from.Decrypt(data)
		if err != nil {
			// A file which the keys it is to have can already read, as
			// one left by a conversion that did not finish, is not a
			// failure of this one.
			if _, done := to.Decrypt(data); to != nil && done == nil {
				return nil
			}
			return err
		}
	}
	converted, err := to.Encrypt(plain)
	if err != nil {
		return err
	}
	if bytes.Equal(converted, data) {
		return nil
	}
	return write(path, converted)
}

// write replaces the file at path atomically. The file can be read and
// written by the user alone.
func write(path string, data []byte) error {
	// A temporary file is made with just those permissions.
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmp.Name(), path)
}
