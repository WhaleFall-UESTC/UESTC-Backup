package tasks

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"client/internal/backup"
	"client/internal/errs"
)

// Task 是一条备份任务。口令不落盘，跑的时候单独给
type Task struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Algorithm   string `json:"algorithm"`
}

// Load 读出任务列表；文件不存在时是空列表。字段名不认识按坏配置报错
func Load(path string) ([]Task, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var list []Task
	if err := dec.Decode(&list); err != nil {
		return nil, errs.Wrapf(err, errs.InvalidArgument, "task file is invalid").At(path)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errs.New(errs.InvalidArgument).At(path)
	}
	return list, nil
}

// Save 把任务列表写回文件。先写临时文件再改名，失败不留半截
func Save(path string, list []Task) error {
	if list == nil {
		list = []Task{}
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Add 追加一条任务，名字和已有的重复时报错。
// Source 和 Destination 转成绝对路径再落盘，任务换个目录执行还指向同一处
func Add(list []Task, t Task) ([]Task, error) {
	t, err := absTask(t)
	if err != nil {
		return nil, err
	}
	if err := Validate(t); err != nil {
		return nil, err
	}
	if Find(list, t.Name) >= 0 {
		return nil, errs.Newf(errs.InvalidArgument, "task %q already exists", t.Name)
	}
	return append(list, t), nil
}

// absTask 把 Source 和 Destination 转成绝对路径
func absTask(t Task) (Task, error) {
	var err error
	if t.Source, err = filepath.Abs(t.Source); err != nil {
		return Task{}, err
	}
	if t.Destination, err = filepath.Abs(t.Destination); err != nil {
		return Task{}, err
	}
	return t, nil
}

// Remove 删掉一条任务，名字不存在时报错
func Remove(list []Task, name string) ([]Task, error) {
	i := Find(list, name)
	if i < 0 {
		return nil, errs.Newf(errs.InvalidArgument, "task %q not found", name)
	}
	return append(list[:i], list[i+1:]...), nil
}

// Find 返回任务的下标，没有时是 -1
func Find(list []Task, name string) int {
	for i, t := range list {
		if t.Name == name {
			return i
		}
	}
	return -1
}

// Validate 检查一条任务的必填项和路径关系
func Validate(t Task) error {
	if t.Name == "" {
		return errs.Newf(errs.InvalidArgument, "task name is empty")
	}
	if t.Source == "" {
		return errs.Newf(errs.InvalidArgument, "task %q: source is empty", t.Name)
	}
	info, err := os.Stat(t.Source)
	if err != nil {
		return errs.Wrapf(err, errs.InvalidArgument, "task %q: source", t.Name)
	}
	if !info.IsDir() {
		return errs.Newf(errs.InvalidArgument, "task %q: source is not a directory", t.Name)
	}
	if t.Destination == "" {
		return errs.Newf(errs.InvalidArgument, "task %q: destination is empty", t.Name)
	}
	if err := backup.CheckDestination(t.Destination, t.Source); err != nil {
		return err
	}
	return nil
}
