// Copyright 2019 RetailNext, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package writefile

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

type WriteOperation func(file *os.File) error

const DefaultDirectoryMode os.FileMode = 0755
const DefaultFileMode os.FileMode = 0644
const DefaultTempPattern = ".temp*~"

type Config struct {
	Directory                string
	DirectoryMode            os.FileMode
	DirectoryUID             int
	DirectoryGID             int
	EnsureDirectoryOwnership bool
	FileMode                 os.FileMode
	FileUID                  int
	FileGID                  int
	EnsureFileOwnership      bool
	TempPattern              string
}

func (c Config) EnsureDirectory() error {
	if c.Directory == "" {
		panic(`writefile: Directory must not be ""`)
	}
	if !filepath.IsAbs(c.Directory) {
		panic(`writefile: Directory must be absolute`)
	}

	info, err := os.Stat(c.Directory)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}

		err = c.EnsureDirectoryIfNotExist()
		if err != nil {
			return err
		}

		info, err = os.Stat(c.Directory)
		if err != nil {
			return err
		}
	}

	if info.Mode() != c.getDirectoryMode()|os.ModeDir {
		err = os.Chmod(c.Directory, c.getDirectoryMode())
		if err != nil {
			return err
		}
	}

	if c.EnsureDirectoryOwnership {
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			if stat.Uid != uint32(c.DirectoryUID) || stat.Gid != uint32(c.DirectoryGID) {
				err = os.Chown(c.Directory, c.DirectoryUID, c.DirectoryGID)
				if err != nil {
					return err
				}
			}
		} else {
			panic("writefile: unable to check directory ownership")
		}
	}

	return nil
}

func (c Config) EnsureDirectoryIfNotExist() error {
	if c.Directory == "" {
		panic(`writefile: Directory must not be ""`)
	}
	if !filepath.IsAbs(c.Directory) {
		panic(`writefile: Directory must be absolute`)
	}

	if c.Directory == "/" {
		return nil
	}

	if _, err := os.Stat(c.Directory); err == nil || !os.IsNotExist(err) {
		return err
	}

	if err := c.parentConfig().EnsureDirectoryIfNotExist(); err != nil {
		return err
	}

	// Parent now exists.
	if err := os.Mkdir(c.Directory, c.getDirectoryMode()); err != nil {
		if !os.IsExist(err) {
			return err
		}
	}

	if c.EnsureDirectoryOwnership {
		info, err := os.Stat(c.Directory)
		if err != nil {
			return err
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			if stat.Uid != uint32(c.DirectoryUID) || stat.Gid != uint32(c.DirectoryGID) {
				err = os.Chown(c.Directory, c.DirectoryUID, c.DirectoryGID)
				if err != nil {
					return err
				}
			}
		} else {
			panic("writefile: unable to check directory ownership")
		}
	}

	return nil
}

func (c Config) parentConfig() Config {
	if c.Directory == "" {
		panic(`writefile: Directory must not be ""`)
	}
	if !filepath.IsAbs(c.Directory) {
		panic(`writefile: Directory must be absolute`)
	}

	parentConfig := c
	parentConfig.Directory = filepath.Dir(c.Directory)
	if parentConfig.Directory == c.Directory {
		panic("wtf")
	}
	return parentConfig
}

func (c Config) WriteFile(name string, op WriteOperation) error {
	if c.Directory == "" {
		panic(`writefile: Directory must not be ""`)
	}
	if !filepath.IsAbs(c.Directory) {
		panic(`writefile: Directory must be absolute`)
	}
	if filepath.IsAbs(name) {
		return InvalidName(name)
	}

	fullPath := filepath.Join(c.Directory, name)
	if dir := filepath.Dir(fullPath); dir != c.Directory {
		if !strings.HasPrefix(dir, c.Directory+"/") {
			return InvalidName(name)
		}

		childConfig := c
		childConfig.Directory = dir
		return childConfig.WriteFile(filepath.Base(fullPath), op)
	}

	var f *os.File
	var tmpName string

	defer func() {
		var closeErr, cleanupErr error
		if f != nil {
			closeErr = f.Close()
		}
		if tmpName != "" {
			cleanupErr = os.Remove(tmpName)
		}
		if cleanupErr != nil && !os.IsNotExist(cleanupErr) {
			panic(cleanupErr)
		}
		if closeErr != nil {
			panic(closeErr)
		}
	}()

	var err error
	f, err = ioutil.TempFile(c.Directory, c.getTempPattern())
	if os.IsNotExist(err) {
		err = c.EnsureDirectoryIfNotExist()
		if err != nil {
			return err
		}
		f, err = ioutil.TempFile(c.Directory, c.getTempPattern())
	}
	if err != nil {
		return err
	}
	tmpName = f.Name()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Mode() != c.getFileMode() {
		err = os.Chmod(tmpName, c.getFileMode())
		if err != nil {
			return err
		}
	}

	if c.EnsureFileOwnership {
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			if stat.Uid != uint32(c.FileUID) || stat.Gid != uint32(c.FileGID) {
				err = os.Chown(tmpName, c.FileUID, c.FileGID)
				if err != nil {
					return err
				}
			}
		} else {
			panic("writefile: unable to check file ownership")
		}
	}

	opErr := op(f)
	if opErr != nil {
		return opErr
	}

	err = f.Close()
	f = nil
	if err != nil {
		return err
	}

	err = os.Rename(tmpName, path.Join(c.Directory, name))
	if err == nil {
		tmpName = ""
	}
	return err
}

func (c Config) getTempPattern() string {
	if c.TempPattern != "" {
		return c.TempPattern
	}
	return DefaultTempPattern
}

func (c Config) getFileMode() os.FileMode {
	if c.FileMode != 0 {
		return c.FileMode
	}
	return DefaultFileMode
}

func (c Config) getDirectoryMode() os.FileMode {
	if c.DirectoryMode != 0 {
		return c.DirectoryMode
	}
	return DefaultDirectoryMode
}

type InvalidName string

func (e InvalidName) Error() string {
	return fmt.Sprintf("writefile: invalid name: %q", e)
}
