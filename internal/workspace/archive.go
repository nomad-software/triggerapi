package workspace

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nomad-software/triggerapi/internal/atomicfile"
	"github.com/nomad-software/triggerapi/internal/crypt"
)

// maxArchived is the most a file in an archive can hold, which keeps one
// made to be enormous when unpacked from being read.
const maxArchived = 256 << 20

// Export writes every workspace of a folder to a zip file for someone else:
// each file is decrypted with the keys it has and encrypted to those of
// whoever the archive is for, of which only the public one is needed. The
// workspaces themselves are left as they are. In the archive each workspace
// is a folder, named after it, holding its files.
func Export(dir, archive string, own, recipient *crypt.Keys) error {
	if own == nil || recipient == nil {
		return errors.New("keys are needed to decrypt the files and to encrypt them again")
	}
	names, err := List(dir)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return errors.New("there are no workspaces to export")
	}

	// The archive is made whole before it is written, atomically.
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	for _, name := range names {
		// A workspace with no files yet is still one.
		if _, err := w.Create(name + "/"); err != nil {
			return err
		}
		for _, file := range []string{environmentsFile, requestsFile} {
			if err := exportFile(w, filepath.Join(dir, name, file), name+"/"+file, own, recipient); err != nil {
				return fmt.Errorf("%s of %s: %w", file, name, err)
			}
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	return atomicfile.Write(archive, data.Bytes(), fileMode)
}

// exportFile adds a file to an archive, encrypted to the recipient. A file
// that does not exist is left out.
func exportFile(w *zip.Writer, path, name string, own, recipient *crypt.Keys) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	plain, err := own.Decrypt(data)
	if err != nil {
		return err
	}
	encrypted, err := recipient.Encrypt(plain)
	if err != nil {
		return err
	}
	entry, err := w.Create(name)
	if err != nil {
		return err
	}
	_, err = entry.Write(encrypted)
	return err
}

// Unpack adds the workspaces of an archive made by Export to a folder, and
// returns the names they were given. One whose name is taken is given
// another, so nothing already there is replaced. The files have to be
// readable with the keys, and nothing is added if any is not, or if the
// archive holds anything but workspaces.
func Unpack(dir, archive string, keys *crypt.Keys) ([]string, error) {
	if keys == nil {
		return nil, errors.New("keys are needed to read the files")
	}
	r, err := zip.OpenReader(archive)
	if err != nil {
		return nil, fmt.Errorf("it is not a zip file that can be read: %w", err)
	}
	defer r.Close()

	// Everything is read and checked before anything is written.
	var names []string
	files := map[string]map[string][]byte{}
	for _, entry := range r.File {
		name, file, _ := strings.Cut(strings.TrimSuffix(entry.Name, "/"), "/")
		if err := Valid(name); err != nil {
			return nil, fmt.Errorf("it is not an export of workspaces: it has %q in it", entry.Name)
		}
		if _, seen := files[name]; !seen {
			names = append(names, name)
			files[name] = map[string][]byte{}
		}
		if file == "" && entry.FileInfo().IsDir() {
			continue
		}
		if file != environmentsFile && file != requestsFile || entry.FileInfo().IsDir() {
			return nil, fmt.Errorf("it is not an export of workspaces: it has %q in it", entry.Name)
		}

		data, err := unpackFile(entry, keys)
		if err != nil {
			return nil, fmt.Errorf("%s of %s: %w", file, name, err)
		}
		files[name][file] = data
	}
	if len(names) == 0 {
		return nil, errors.New("there are no workspaces in it")
	}

	var added []string
	for _, name := range names {
		unique := Unique(dir, name)
		if err := Create(dir, unique); err != nil {
			return added, err
		}
		added = append(added, unique)
		for file, data := range files[name] {
			if err := atomicfile.Write(filepath.Join(dir, unique, file), data, fileMode); err != nil {
				return added, err
			}
		}
	}
	return added, nil
}

// unpackFile returns a file of an archive as it is to be kept: as it is if
// the keys can decrypt it, and encrypted with them if it was not encrypted.
func unpackFile(entry *zip.File, keys *crypt.Keys) ([]byte, error) {
	src, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	data, err := io.ReadAll(io.LimitReader(src, maxArchived+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxArchived {
		return nil, errors.New("it is too large")
	}

	plain, err := keys.Decrypt(data)
	if err != nil {
		return nil, fmt.Errorf("it cannot be read with the keys of the settings: %w", err)
	}
	if crypt.Encrypted(data) {
		return data, nil
	}
	return keys.Encrypt(plain)
}
