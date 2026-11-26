package manager

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"client/internal/backup"
	"client/internal/errs"
	"client/internal/tasks"
)

// flatSource 造一个有 n 个小文件的源目录，文件名排序稳定
func flatSource(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < n; i++ {
		name := filepath.Join(dir, fmt.Sprintf("f%04d.txt", i))
		if err := os.WriteFile(name, []byte("data"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// addBackupTask 把一条不压缩的任务写进任务文件并返回它，要先跑过 useTempConfig
func addBackupTask(t *testing.T, name, src, password string) tasks.Task {
	t.Helper()
	task := tasks.Task{
		Name:        name,
		Source:      src,
		Destination: filepath.Join(t.TempDir(), name+".dbkp"),
		Algorithm:   "none",
		Encrypted:   password != "",
	}
	if err := AddTask(task); err != nil {
		t.Fatalf("AddTask(%s) error = %v", name, err)
	}
	return task
}

// startBackup 造一个独立的配置目录、一条任务并起备份作业，返回作业号和任务；压缩算法固定不压缩
func startBackup(t *testing.T, src, password string) (string, tasks.Task) {
	t.Helper()
	useTempConfig(t)
	task := addBackupTask(t, "t", src, password)
	id, err := StartBackup(task.Name, password)
	if err != nil {
		t.Fatalf("StartBackup() error = %v", err)
	}
	return id, task
}

// waitState 轮询到作业不再是 running，超时 30 秒
func waitState(t *testing.T, id string) Snapshot {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		s, err := GetJob(id)
		if err != nil {
			t.Fatalf("GetJob(%s) error = %v", id, err)
		}
		if s.State != StateRunning {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s is still running after 30s", id)
	return Snapshot{}
}

// event 是订阅回调收到的一条记录，at 用来量节流间隔
type event struct {
	at   time.Time
	snap Snapshot
}

// collectEvents 订阅作业事件攒进 channel，返回 channel 和退订函数
func collectEvents(buffer int) (<-chan event, func()) {
	ch := make(chan event, buffer)
	return ch, Subscribe(func(s Snapshot) { ch <- event{at: time.Now(), snap: s} })
}

// waitForEvent 等到一条满足 cond 的事件，超时 10 秒
func waitForEvent(t *testing.T, ch <-chan event, cond func(event) bool) event {
	t.Helper()
	for {
		select {
		case e := <-ch:
			if cond(e) {
				return e
			}
		case <-time.After(10 * time.Second):
			t.Fatal("waited 10s for an event")
			return event{}
		}
	}
}

// drainEvents 把 channel 里已经到位的事件取出来，不等新的
func drainEvents(ch <-chan event) []event {
	var out []event
	for {
		select {
		case e := <-ch:
			out = append(out, e)
		default:
			return out
		}
	}
}

// 备份跑完状态是 done，结果里的文件数和源目录一致，归档落在目标位置，结束事件最后到
func TestBackupJobFinishes(t *testing.T) {
	src := flatSource(t, 3)
	id, task := startBackup(t, src, "")

	ch, unsub := collectEvents(16)
	defer unsub()
	s := waitState(t, id)
	time.Sleep(100 * time.Millisecond) // 结束事件的回调可能还在路上

	if s.State != StateDone {
		t.Fatalf("State = %q, want %q", s.State, StateDone)
	}
	if s.Result == nil {
		t.Fatal("State is done but Result is nil")
	}
	if s.Result.Code != 0 || s.Result.Files != 3 || s.Result.Skipped != 0 {
		t.Errorf("Result = %+v, want code 0, 3 files, no skips", s.Result)
	}
	if s.DoneFiles != 3 || s.TotalFiles != 3 {
		t.Errorf("DoneFiles/TotalFiles = %d/%d, want 3/3", s.DoneFiles, s.TotalFiles)
	}
	if _, err := os.Stat(task.Destination); err != nil {
		t.Errorf("Stat(%s) error = %v", task.Destination, err)
	}

	evs := drainEvents(ch)
	if len(evs) == 0 {
		t.Fatal("no events received")
	}
	if last := evs[len(evs)-1].snap; last.State != StateDone || last.ID != id {
		t.Errorf("last event = %+v, want the done snapshot of job %s", last, id)
	}
}

// 跑到一半取消，状态是 cancelled，目标目录不留半截归档
func TestCancelBackupJob(t *testing.T) {
	src := flatSource(t, 3000)
	id, task := startBackup(t, src, "")

	ch, unsub := collectEvents(4096)
	defer unsub()
	waitForEvent(t, ch, func(e event) bool { return e.snap.DoneFiles > 0 })

	if err := Cancel(id); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	s := waitState(t, id)
	if s.State != StateCancelled {
		t.Fatalf("State = %q, want %q", s.State, StateCancelled)
	}
	if s.Result != nil {
		t.Errorf("cancelled job has Result %+v, want nil", s.Result)
	}

	entries, err := os.ReadDir(filepath.Dir(task.Destination))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("destination keeps half-written file %s", e.Name())
		}
	}
	if _, err := os.Stat(task.Destination); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Stat(%s) after cancel = %v, want not exist", task.Destination, err)
	}
}

// 退订之后不再收到这个作业的任何事件
func TestUnsubscribeStopsEvents(t *testing.T) {
	ch, unsub := collectEvents(8)
	unsub()

	id, _ := startBackup(t, flatSource(t, 3), "")
	waitState(t, id)
	time.Sleep(150 * time.Millisecond)

	if evs := drainEvents(ch); len(evs) != 0 {
		t.Errorf("got %d events after unsubscribe, want 0", len(evs))
	}
}

// 正确口令校验通过，错误口令作业失败且类别是 WrongPassword，口令不出现在结果里
func TestVerifyJobWithPasswords(t *testing.T) {
	id, task := startBackup(t, flatSource(t, 3), "pw")
	if s := waitState(t, id); s.State != StateDone {
		t.Fatalf("backup State = %q, want %q", s.State, StateDone)
	}

	id, err := StartVerify(task.Destination, "pw")
	if err != nil {
		t.Fatalf("StartVerify() error = %v", err)
	}
	if s := waitState(t, id); s.State != StateDone || s.Result.Files != 3 {
		t.Errorf("verify State = %q, Result = %+v, want done with 3 files", s.State, s.Result)
	}

	id, err = StartVerify(task.Destination, "wrong")
	if err != nil {
		t.Fatalf("StartVerify() error = %v", err)
	}
	s := waitState(t, id)
	if s.State != StateFailed {
		t.Fatalf("State = %q, want %q", s.State, StateFailed)
	}
	if s.Result == nil || s.Result.Code != int(errs.WrongPassword) {
		t.Errorf("Result = %+v, want code WrongPassword", s.Result)
	}
	if s.Result != nil && strings.Contains(s.Result.Message, "pw") {
		t.Errorf("failure message leaks the password: %q", s.Result.Message)
	}
}

// 整份还原：文件数对得上，内容和源目录一致
func TestRestoreJobRestoresWholeArchive(t *testing.T) {
	id, task := startBackup(t, flatSource(t, 3), "")
	if s := waitState(t, id); s.State != StateDone {
		t.Fatalf("backup State = %q, want %q", s.State, StateDone)
	}

	dst := t.TempDir()
	id, err := StartRestore(task.Destination, dst, "", backup.ConflictFail)
	if err != nil {
		t.Fatalf("StartRestore() error = %v", err)
	}
	s := waitState(t, id)
	if s.State != StateDone || s.Result == nil || s.Result.Files != 3 {
		t.Fatalf("restore State = %q, Result = %+v, want done with 3 files", s.State, s.Result)
	}
	for i := 0; i < 3; i++ {
		name := filepath.Join(dst, fmt.Sprintf("f%04d.txt", i))
		data, err := os.ReadFile(name)
		if err != nil || string(data) != "data" {
			t.Errorf("restored %s = %q, %v", name, data, err)
		}
	}
}

// 作业进行中再起备份、校验、还原都被拒，类别是 JobBusy；作业结束后能再起
func TestSecondJobRejectedWhileRunning(t *testing.T) {
	useTempConfig(t)
	task := addBackupTask(t, "a", flatSource(t, 3000), "")
	id, err := StartBackup(task.Name, "")
	if err != nil {
		t.Fatalf("StartBackup() error = %v", err)
	}

	addBackupTask(t, "b", flatSource(t, 1), "")
	if _, err := StartBackup("b", ""); !errors.Is(err, errs.ErrJobBusy) {
		t.Errorf("second StartBackup: error = %v, want errs.ErrJobBusy", err)
	}
	if _, err := StartVerify(task.Destination, ""); !errors.Is(err, errs.ErrJobBusy) {
		t.Errorf("StartVerify while running: error = %v, want errs.ErrJobBusy", err)
	}
	if _, err := StartRestore(task.Destination, t.TempDir(), "", backup.ConflictSkip); !errors.Is(err, errs.ErrJobBusy) {
		t.Errorf("StartRestore while running: error = %v, want errs.ErrJobBusy", err)
	}

	if err := Cancel(id); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	waitState(t, id)

	// 任务 a 被取消，归档没落盘，这次校验会失败，但说明忙检查已经解除
	id2, err := StartVerify(task.Destination, "")
	if err != nil {
		t.Fatalf("StartVerify after cancel: error = %v, want nil", err)
	}
	if s := waitState(t, id2); s.State != StateFailed {
		t.Errorf("verify State = %q, want %q", s.State, StateFailed)
	}
}

// 同一阶段内最多每 100 毫秒发一条，阶段变化必发，用假时钟跑，不依赖机器快慢
func TestProgressThrottle(t *testing.T) {
	now := time.Unix(0, 0)
	pt := progressThrottle{now: func() time.Time { return now }}

	if !pt.allow(backup.Progress{Phase: backup.PhasePacking, DoneFiles: 1}) {
		t.Error("the first progress in a phase was dropped")
	}
	for i := 2; i <= 10; i++ {
		now = now.Add(10 * time.Millisecond)
		if pt.allow(backup.Progress{Phase: backup.PhasePacking, DoneFiles: i}) {
			t.Errorf("progress %d published %dms after the last one", i, 10*(i-1))
		}
	}
	now = now.Add(10 * time.Millisecond)
	if !pt.allow(backup.Progress{Phase: backup.PhasePacking, DoneFiles: 11}) {
		t.Error("progress 100ms after the last one was dropped")
	}
	if pt.allow(backup.Progress{Phase: backup.PhasePacking, DoneFiles: 12}) {
		t.Error("progress published right after the last one")
	}
	now = now.Add(time.Millisecond)
	if !pt.allow(backup.Progress{Phase: backup.PhaseCompressing}) {
		t.Error("a phase change was dropped")
	}
}

// 真实备份里 DoneFiles 不回退；若同一阶段收到相邻两条，间隔不小于节流间隔；结束帧计数走到头
func TestProgressMonotonic(t *testing.T) {
	id, _ := startBackup(t, flatSource(t, 3000), "")

	ch, unsub := collectEvents(128)
	defer unsub()
	s := waitState(t, id)
	time.Sleep(150 * time.Millisecond)

	evs := drainEvents(ch)
	for i := 1; i < len(evs); i++ {
		prev, e := evs[i-1], evs[i]
		if e.snap.State != StateRunning || prev.snap.State != StateRunning || e.snap.Phase != prev.snap.Phase {
			continue
		}
		if e.snap.DoneFiles < prev.snap.DoneFiles {
			t.Errorf("DoneFiles went back: %d after %d in phase %s", e.snap.DoneFiles, prev.snap.DoneFiles, e.snap.Phase)
		}
		// 回调记账比节流判定晚一点，留 1 毫秒余量
		if gap := e.at.Sub(prev.at); gap < progressInterval-time.Millisecond {
			t.Errorf("two progress events %v apart in phase %s, want at least %v", gap, e.snap.Phase, progressInterval)
		}
	}

	if s.DoneFiles != 3000 || s.TotalFiles != 3000 || s.Result.Files != 3000 {
		t.Errorf("final = %d/%d files, Result = %+v, want 3000", s.DoneFiles, s.TotalFiles, s.Result)
	}
}

// Snapshots 按启动顺序返回全部作业，状态跟着作业走，界面重载后靠它恢复
func TestSnapshotsListsAllJobs(t *testing.T) {
	useTempConfig(t)
	jobsMu.Lock()
	jobs = map[string]*job{}
	jobsMu.Unlock()

	task := addBackupTask(t, "a", flatSource(t, 3000), "")
	first, err := StartBackup(task.Name, "")
	if err != nil {
		t.Fatalf("StartBackup() error = %v", err)
	}
	if snaps := Snapshots(); len(snaps) != 1 || snaps[0].ID != first || snaps[0].State != StateRunning {
		t.Errorf("Snapshots() while running = %+v, want one running job %s", snaps, first)
	}

	if err := Cancel(first); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	waitState(t, first)

	secondTask := addBackupTask(t, "b", flatSource(t, 1), "")
	second, err := StartBackup(secondTask.Name, "")
	if err != nil {
		t.Fatalf("StartBackup() error = %v", err)
	}
	if s := waitState(t, second); s.State != StateDone {
		t.Fatalf("State = %q, want %q", s.State, StateDone)
	}

	snaps := Snapshots()
	if len(snaps) != 2 {
		t.Fatalf("Snapshots() = %+v, want 2 jobs", snaps)
	}
	if snaps[0].ID != first || snaps[0].State != StateCancelled {
		t.Errorf("Snapshots()[0] = %+v, want cancelled job %s", snaps[0], first)
	}
	if snaps[1].ID != second || snaps[1].State != StateDone {
		t.Errorf("Snapshots()[1] = %+v, want done job %s", snaps[1], second)
	}
}
