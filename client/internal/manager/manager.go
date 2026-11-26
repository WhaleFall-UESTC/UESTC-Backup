package manager

import (
	"os"
	"path/filepath"
	"sync"

	"client/internal/tasks"
)

// configDir 返回界面的配置目录，任务文件、运行记录、设置都在这里，不跟着当前目录走；测试里换成临时目录
var configDir = func() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "UESTC-Backup"), nil
}

// tasksMu 把任务文件的读改写串起来，并发的调用不会互相覆盖
var tasksMu sync.Mutex

// TasksPath 返回任务文件的全路径
func TasksPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tasks.json"), nil
}

// ListTasks 读出全部任务；还没有任务文件时是空列表
func ListTasks() ([]tasks.Task, error) {
	tasksMu.Lock()
	defer tasksMu.Unlock()
	path, err := TasksPath()
	if err != nil {
		return nil, err
	}
	return tasks.Load(path)
}

// AddTask 新增一条任务并落盘，名字重复报错
func AddTask(t tasks.Task) error {
	tasksMu.Lock()
	defer tasksMu.Unlock()
	return rewriteTasks(func(list []tasks.Task) ([]tasks.Task, error) {
		return tasks.Add(list, t)
	})
}

// SaveTask 把名为 name 的任务换成 t，换名字、换源目录或换归档路径都会重新校验；任务不存在报错
func SaveTask(name string, t tasks.Task) error {
	tasksMu.Lock()
	defer tasksMu.Unlock()
	return rewriteTasks(func(list []tasks.Task) ([]tasks.Task, error) {
		return tasks.Update(list, name, t)
	})
}

// DeleteTask 删掉一条任务并落盘，名字不存在报错
func DeleteTask(name string) error {
	tasksMu.Lock()
	defer tasksMu.Unlock()
	return rewriteTasks(func(list []tasks.Task) ([]tasks.Task, error) {
		return tasks.Remove(list, name)
	})
}

// rewriteTasks 读出任务文件，交给 alter 改，再写回去；配置目录不存在就先建
func rewriteTasks(alter func([]tasks.Task) ([]tasks.Task, error)) error {
	path, err := TasksPath()
	if err != nil {
		return err
	}
	list, err := tasks.Load(path)
	if err != nil {
		return err
	}
	if list, err = alter(list); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return tasks.Save(path, list)
}
