// Package bootstrap provides the local setup UI before PostgreSQL is configured.
package bootstrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Autumn-27/artex/config"
)

// SaveDatabase preserves unknown settings and an exclusive backup of the old file.
// It never replaces malformed config or a file changed since it was read.
func SaveDatabase(path string, database config.Database) error {
	if _, err := database.ConnectionString(); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("配置文件必须为普通文件，不能使用链接或目录")
	}
	original, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return errors.New("无法读取配置文件；原文件已保留")
	}
	existed := err == nil
	fields := map[string]json.RawMessage{}
	if existed {
		if len(original) > 1<<20 || json.Unmarshal(original, &fields) != nil || fields == nil {
			return errors.New("配置 JSON 无效；请先修复，原文件已保留")
		}
	}
	encoded, err := json.Marshal(database)
	if err != nil {
		return err
	}
	fields["database"] = encoded
	output, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return err
	}
	output = append(output, '\n')
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".diting-config-*")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	_, writeErr := file.Write(output)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	current, readErr := os.ReadFile(path)
	if (existed && (readErr != nil || !bytes.Equal(current, original))) || (!existed && !os.IsNotExist(readErr)) {
		return errors.New("配置文件已变化；未覆盖，请重新检查")
	}
	if existed {
		backup := fmt.Sprintf("%s.before-setup-%d.backup", path, time.Now().UnixNano())
		f, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr = f.Write(original)
		syncErr = f.Sync()
		closeErr = f.Close()
		if writeErr != nil {
			return writeErr
		}
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
		if err = os.Rename(tmp, path); err != nil {
			return errors.New("无法替换配置；原文件及备份已保留")
		}
		return nil
	}
	// Hard-link creation is exclusive: another writer cannot be overwritten.
	if err = os.Link(tmp, path); err != nil {
		return errors.New("无法创建配置；未覆盖任何已有文件")
	}
	return nil
}
