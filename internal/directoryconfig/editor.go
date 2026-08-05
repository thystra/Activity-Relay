package directoryconfig

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/thystra/Activity-Relay/internal/directoryclient"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

const BackupSuffix = ".activity-relay.bak"

type editAction uint8

const (
	disableEntry editAction = iota + 1
	removeEntry
)

type writeStage uint8

const (
	stagePrepared writeStage = iota + 1
	stageBackupDurable
	stageRenamed
	stageDirectorySynced
)

type stageHook func(writeStage) error

func DisableFile(path, origin string) (string, error) {
	return editFile(path, origin, disableEntry, false, nil)
}

func RemoveFile(path, origin string) (string, error) {
	return editFile(path, origin, removeEntry, true, nil)
}

func editFile(
	path, origin string,
	action editAction,
	preserveBackup bool,
	hook stageHook,
) (string, error) {
	parsedOrigin, err := directoryclient.ParseOrigin(origin)
	if err != nil {
		return "", ErrConfiguration
	}
	locked, info, original, err := openLockedSnapshot(path)
	if err != nil {
		return "", ErrConfiguration
	}
	defer func() {
		_ = unix.Flock(int(locked.Fd()), unix.LOCK_UN)
		_ = locked.Close()
	}()
	root, err := decodeYAML(original)
	if err != nil {
		return "", err
	}
	directories := mappingValue(root, "DIRECTORIES")
	if directories == nil || directories.Kind != yaml.SequenceNode {
		return "", ErrNotFound
	}
	if _, err := parseDirectorySequence(directories); err != nil {
		return "", err
	}
	entryIndex := -1
	entryWasEnabled := false
	for index, item := range directories.Content {
		originNode := mappingValue(item, "origin")
		if originNode != nil && originNode.Value == parsedOrigin.String() {
			entryIndex = index
			if action == disableEntry {
				enabledNode := mappingValue(item, "enabled")
				if enabledNode != nil {
					_ = enabledNode.Decode(&entryWasEnabled)
				}
				setEnabled(item, false)
			}
			break
		}
	}
	if entryIndex < 0 {
		return "", ErrNotFound
	}
	backup := path + BackupSuffix
	if action == disableEntry && !entryWasEnabled && regularFileExists(backup) {
		return backup, nil
	}
	if action == removeEntry {
		directories.Content = append(
			directories.Content[:entryIndex],
			directories.Content[entryIndex+1:]...,
		)
	}
	var encoded bytes.Buffer
	encoder := yaml.NewEncoder(&encoded)
	encoder.SetIndent(2)
	if err := encoder.Encode(root); err != nil {
		return "", ErrConfiguration
	}
	if err := encoder.Close(); err != nil {
		return "", ErrConfiguration
	}
	if err := atomicReplace(
		path, backup, original, encoded.Bytes(), info,
		preserveBackup || (action == disableEntry && !entryWasEnabled), hook,
	); err != nil {
		return "", err
	}
	return backup, nil
}

func openLockedSnapshot(path string) (*os.File, os.FileInfo, []byte, error) {
	for attempt := 0; attempt < 4; attempt++ {
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return nil, nil, nil, err
		}
		file := os.NewFile(uintptr(fd), "locked configuration")
		if file == nil {
			_ = unix.Close(fd)
			return nil, nil, nil, ErrConfiguration
		}
		if err := unix.Flock(fd, unix.LOCK_EX); err != nil {
			_ = file.Close()
			return nil, nil, nil, err
		}
		info, statErr := file.Stat()
		current, pathErr := os.Lstat(path)
		if statErr == nil && pathErr == nil && info.Mode().IsRegular() &&
			current.Mode().IsRegular() && os.SameFile(info, current) {
			if info.Size() > maximumConfigBytes {
				_ = unix.Flock(fd, unix.LOCK_UN)
				_ = file.Close()
				return nil, nil, nil, ErrConfiguration
			}
			body, readErr := io.ReadAll(io.LimitReader(file, maximumConfigBytes+1))
			if readErr != nil || int64(len(body)) > maximumConfigBytes {
				_ = unix.Flock(fd, unix.LOCK_UN)
				_ = file.Close()
				return nil, nil, nil, ErrConfiguration
			}
			return file, info, body, nil
		}
		_ = unix.Flock(fd, unix.LOCK_UN)
		_ = file.Close()
	}
	return nil, nil, nil, ErrConfiguration
}

func setEnabled(entry *yaml.Node, enabled bool) {
	value := mappingValue(entry, "enabled")
	if value == nil {
		entry.Content = append(entry.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "enabled"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool"},
		)
		value = entry.Content[len(entry.Content)-1]
	}
	value.Kind = yaml.ScalarNode
	value.Tag = "!!bool"
	value.Value = "false"
}

func atomicReplace(
	path, backupPath string,
	original, replacement []byte,
	info os.FileInfo,
	preserveBackup bool,
	hook stageHook,
) error {
	directory := filepath.Dir(path)
	mode := info.Mode()
	uid, gid, err := ownership(info)
	if err != nil {
		return ErrConfiguration
	}
	replacementTemp, err := writeSyncedTemp(directory, filepath.Base(path), replacement, mode, uid, gid)
	if err != nil {
		return ErrConfiguration
	}
	defer os.Remove(replacementTemp)
	if err := runStageHook(hook, stagePrepared); err != nil {
		return err
	}
	if !preserveBackup || !regularFileExists(backupPath) {
		backupTemp, err := writeSyncedTemp(directory, filepath.Base(backupPath), original, mode, uid, gid)
		if err != nil {
			return ErrConfiguration
		}
		defer os.Remove(backupTemp)
		if err := os.Rename(backupTemp, backupPath); err != nil {
			return ErrConfiguration
		}
		if err := syncDirectory(directory); err != nil {
			return ErrConfiguration
		}
	}
	if err := runStageHook(hook, stageBackupDurable); err != nil {
		return err
	}
	current, err := os.Lstat(path)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(info, current) {
		return ErrConfiguration
	}
	if err := os.Rename(replacementTemp, path); err != nil {
		return ErrConfiguration
	}
	if err := runStageHook(hook, stageRenamed); err != nil {
		return err
	}
	if err := syncDirectory(directory); err != nil {
		return ErrConfiguration
	}
	if err := runStageHook(hook, stageDirectorySynced); err != nil {
		return err
	}
	return nil
}

func regularFileExists(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}

func writeSyncedTemp(
	directory, base string,
	body []byte,
	mode os.FileMode,
	uid, gid int,
) (string, error) {
	file, err := os.CreateTemp(directory, "."+base+".tmp-")
	if err != nil {
		return "", err
	}
	path := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := io.Copy(file, bytes.NewReader(body)); err != nil {
		return "", err
	}
	if err := file.Chown(uid, gid); err != nil {
		return "", err
	}
	if err := file.Chmod(mode); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	ok = true
	return path, nil
}

func ownership(info os.FileInfo) (int, int, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, ErrConfiguration
	}
	return int(stat.Uid), int(stat.Gid), nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func runStageHook(hook stageHook, stage writeStage) error {
	if hook == nil {
		return nil
	}
	if err := hook(stage); err != nil {
		return errors.Join(ErrConfiguration, err)
	}
	return nil
}
