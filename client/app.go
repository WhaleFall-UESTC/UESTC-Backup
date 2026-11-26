package main

import (
	"context"
	"errors"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"client/internal/backup"
	"client/internal/errs"
	"client/internal/manager"
	"client/internal/tasks"
)

// App 是绑定层：方法只转调 manager，进度经订阅转成前端事件，业务都在 manager 里
type App struct {
	ctx      context.Context
	unsubJob func()
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup 保存上下文并订阅作业事件，之后前端经 job:progress、job:finished 收快照
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.unsubJob = manager.Subscribe(func(s manager.Snapshot) {
		name := "job:progress"
		if s.State != manager.StateRunning {
			name = "job:finished"
		}
		runtime.EventsEmit(a.ctx, name, s)
	})
}

// shutdown 退订作业事件，窗口关掉后不再往死掉的上下文发事件
func (a *App) shutdown(_ context.Context) {
	a.unsubJob()
}

// Reply 是不带数据的返回值，Code 是 errs 的类别码，0 是成功
type Reply struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

// TasksReply 是任务列表的返回值
type TasksReply struct {
	Reply
	Tasks []tasks.Task `json:"tasks"`
}

// JobReply 是起作业的返回值，成功时带作业号
type JobReply struct {
	Reply
	JobID string `json:"job_id,omitempty"`
}

// reply 把错误折进返回值，前端拿类别码区分，不去匹配文字
func reply(err error) Reply {
	if err == nil {
		return Reply{}
	}
	code := int(errs.IO)
	var e *errs.Error
	if errors.As(err, &e) {
		code = int(e.Code)
	}
	return Reply{Code: code, Message: err.Error()}
}

// ListTasks 读出全部任务；还没有任务文件时是空列表，不让 null 到前端
func (a *App) ListTasks() TasksReply {
	list, err := manager.ListTasks()
	if err != nil {
		return TasksReply{Reply: reply(err)}
	}
	if list == nil {
		list = []tasks.Task{}
	}
	return TasksReply{Tasks: list}
}

// AddTask 新增一条任务，名字重复或校验不过时报错
func (a *App) AddTask(task tasks.Task) Reply {
	return reply(manager.AddTask(task))
}

// SaveTask 把名为 name 的任务换成 task，换名字、换路径都会重新校验
func (a *App) SaveTask(name string, task tasks.Task) Reply {
	return reply(manager.SaveTask(name, task))
}

// DeleteTask 删掉一条任务，只删任务不删归档
func (a *App) DeleteTask(name string) Reply {
	return reply(manager.DeleteTask(name))
}

// StartBackup 起一个备份作业，成功时带作业号，进度和结果看事件
func (a *App) StartBackup(name, password string) JobReply {
	return startJob(manager.StartBackup(name, password))
}

// StartVerify 起一个校验作业，成功时带作业号
func (a *App) StartVerify(archive, password string) JobReply {
	return startJob(manager.StartVerify(archive, password))
}

// StartRestore 起一个整份还原的作业。conflict 取 backup.Conflict 的值：
// 0 目标非空就拒绝，1 覆盖，2 跳过，3 另存新名字
func (a *App) StartRestore(archive, dst, password string, conflict int) JobReply {
	return startJob(manager.StartRestore(archive, dst, password, backup.Conflict(conflict)))
}

// CancelJob 请作业停下来
func (a *App) CancelJob(id string) Reply {
	return reply(manager.Cancel(id))
}

// GetJobs 返回全部作业的快照，界面重载后恢复用
func (a *App) GetJobs() []manager.Snapshot {
	return manager.Snapshots()
}

// startJob 把 (作业号, 错误) 折成返回值
func startJob(id string, err error) JobReply {
	if err != nil {
		return JobReply{Reply: reply(err)}
	}
	return JobReply{JobID: id}
}

// PickFolder 弹系统的目录选择框，用户取消时是空串
func (a *App) PickFolder(title string) string {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
	if err != nil {
		return ""
	}
	return dir
}

// PickArchive 弹系统的归档选择框，只认 .dbkp，用户取消时是空串
func (a *App) PickArchive(title string) string {
	file, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   title,
		Filters: []runtime.FileFilter{{DisplayName: "备份归档 (*.dbkp)", Pattern: "*.dbkp"}},
	})
	if err != nil {
		return ""
	}
	return file
}
