package manager

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"client/internal/backup"
	"client/internal/errs"
	"client/internal/tasks"
)

// 作业的类别和状态，值原样发给前端
const (
	KindBackup  = "backup"
	KindVerify  = "verify"
	KindRestore = "restore"

	StateRunning   = "running"
	StateDone      = "done"
	StateFailed    = "failed"
	StateCancelled = "cancelled"
)

// progressInterval 是同一阶段两条进度事件的最小间隔，和命令行的节流一致
const progressInterval = 100 * time.Millisecond

// SkipInfo 是一条被跳过的节点和原因
type SkipInfo struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Result 是一次作业的结果，正常结束和失败才有，取消的作业没有
type Result struct {
	Files       int           `json:"files"`
	Renamed     int           `json:"renamed"`
	Skipped     int           `json:"skipped"`
	Bytes       int64         `json:"bytes"`
	Skips       []SkipInfo    `json:"skips"`
	Unsupported []string      `json:"unsupported"`
	Code        int           `json:"code"`
	Message     string        `json:"message"`
	Duration    time.Duration `json:"duration"`
}

// Snapshot 是作业在某一刻的完整状态，事件和查询发的都是它
type Snapshot struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"`
	State      string  `json:"state"`
	Task       string  `json:"task"`
	Phase      string  `json:"phase"`
	Path       string  `json:"path"`
	DoneFiles  int     `json:"done_files"`
	TotalFiles int     `json:"total_files"`
	DoneBytes  int64   `json:"done_bytes"`
	TotalBytes int64   `json:"total_bytes"`
	Result     *Result `json:"result"`
}

var (
	jobsMu    sync.Mutex
	jobs      = map[string]*job{}
	nextJobID int
	subs      = map[int]func(Snapshot){}
	nextSubID int
)

// job 是一个作业的可变状态，读写都在 jobsMu 之内
type job struct {
	id     string
	kind   string
	task   string
	state  string
	prog   backup.Progress
	result *Result
	cancel context.CancelFunc
}

// snapshotValue 取一份完整的作业状态，锁内使用；Result 结束后就不再变，可以带到锁外用
func (j *job) snapshotValue() Snapshot {
	return Snapshot{
		ID:         j.id,
		Kind:       j.kind,
		State:      j.state,
		Task:       j.task,
		Phase:      j.prog.Phase.String(),
		Path:       j.prog.Path,
		DoneFiles:  j.prog.DoneFiles,
		TotalFiles: j.prog.TotalFiles,
		DoneBytes:  j.prog.DoneBytes,
		TotalBytes: j.prog.TotalBytes,
		Result:     j.result,
	}
}

// taskAlgorithms 把任务文件里的算法名换成引擎的枚举
var taskAlgorithms = map[string]backup.Algorithm{
	"none":    backup.AlgorithmNone,
	"lz77":    backup.AlgorithmLZ77,
	"huffman": backup.AlgorithmHuffman,
}

// StartBackup 起一个备份作业并返回作业号。任务按名字从任务文件里取，
// 不存在、校验不过或加密任务没给口令当场报错
func StartBackup(name, password string) (string, error) {
	list, err := ListTasks()
	if err != nil {
		return "", err
	}
	i := tasks.Find(list, name)
	if i < 0 {
		return "", errs.Newf(errs.InvalidArgument, "task %q not found", name)
	}
	t := list[i]
	if t.Encrypted && password == "" {
		return "", errs.Newf(errs.InvalidArgument, "task %q is encrypted, password is required", t.Name)
	}
	if err := tasks.Validate(t); err != nil {
		return "", err
	}
	algo, ok := taskAlgorithms[t.Algorithm]
	if !ok {
		return "", errs.Newf(errs.InvalidArgument, "task %q: unknown algorithm %q", t.Name, t.Algorithm)
	}
	// 归档加没加密跟着任务标记走，没标记的任务不给口令，密文归档不会被悄悄换成明文
	if !t.Encrypted {
		password = ""
	}
	return start(KindBackup, t.Name, func(ctx context.Context, emit func(backup.Progress)) (*Result, error) {
		return runBackup(ctx, t, algo, password, emit)
	})
}

// runBackup 扫描源目录并打包，扫描和打包跳过的节点都记进结果，和命令行退 9 用的是同一份
func runBackup(ctx context.Context, t tasks.Task, algo backup.Algorithm, password string, emit func(backup.Progress)) (*Result, error) {
	entries, scanSkips, err := backup.Scan(t.Source)
	if err != nil {
		return nil, err
	}
	var last backup.Progress
	opts := backup.PackOptions{
		Algorithm: algo,
		Password:  password,
		Progress:  func(p backup.Progress) { last = p; emit(p) },
	}
	packSkips, err := backup.Pack(ctx, t.Destination, t.Source, entries, opts)
	if err != nil {
		return nil, err
	}
	skips := append(scanSkips, packSkips...)
	res := &Result{
		Files:   len(entries) - len(packSkips),
		Skipped: len(skips),
		Skips:   skipInfos(skips),
		Bytes:   last.DoneBytes,
	}
	if len(skips) > 0 {
		res.Code = int(errs.PartialBackup)
	}
	return res, nil
}

// skipInfos 把引擎的跳过记录换成给前端的样子
func skipInfos(skips []backup.Skip) []SkipInfo {
	var out []SkipInfo
	for _, s := range skips {
		out = append(out, SkipInfo{Path: s.Path, Reason: s.Err.Error()})
	}
	return out
}

// StartVerify 起一个校验作业并返回作业号
func StartVerify(archive, password string) (string, error) {
	return start(KindVerify, "", func(ctx context.Context, emit func(backup.Progress)) (*Result, error) {
		return runVerify(ctx, archive, password, emit)
	})
}

// StartRestore 起一个整份还原的作业并返回作业号，conflict 是遇到已有文件的处理办法
func StartRestore(archive, dst, password string, conflict backup.Conflict) (string, error) {
	return start(KindRestore, "", func(ctx context.Context, emit func(backup.Progress)) (*Result, error) {
		var last backup.Progress
		opts := backup.UnpackOptions{
			Conflict: conflict,
			Progress: func(p backup.Progress) { last = p; emit(p) },
		}
		r, err := backup.Unpack(ctx, archive, dst, password, opts)
		if err != nil {
			return nil, err
		}
		return &Result{Files: r.Restored, Renamed: r.Renamed, Skipped: r.Skipped, Bytes: last.DoneBytes}, nil
	})
}

// runVerify 把整份归档核对一遍，文件数和字节数取进度走到的位置
func runVerify(ctx context.Context, archive, password string, emit func(backup.Progress)) (*Result, error) {
	var last backup.Progress
	err := backup.Verify(ctx, archive, password, backup.VerifyOptions{
		Progress: func(p backup.Progress) { last = p; emit(p) },
	})
	if err != nil {
		return nil, err
	}
	return &Result{Files: last.DoneFiles, Bytes: last.DoneBytes}, nil
}

// start 登记一个作业并让它在自己的 goroutine 里跑，返回作业号。
// 同一时间只跑一个作业：归档被读着的时候，Windows 上备份最后一步的改名覆盖会失败
func start(kind, taskName string, exec func(ctx context.Context, emit func(backup.Progress)) (*Result, error)) (string, error) {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	for _, other := range jobs {
		if other.state == StateRunning {
			return "", errs.Newf(errs.JobBusy, "job %s is still running", other.id)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	nextJobID++
	j := &job{
		id:     fmt.Sprintf("job-%d", nextJobID),
		kind:   kind,
		task:   taskName,
		state:  StateRunning,
		cancel: cancel,
	}
	jobs[j.id] = j
	go runJob(ctx, j, exec)
	return j.id, nil
}

// progressThrottle 决定一条进度要不要发布：阶段变化必发，同一阶段至少隔 progressInterval。
// 只被作业的 goroutine 触碰；now 可以替换，测试里用假时钟
type progressThrottle struct {
	lastPhase backup.Phase
	lastEmit  time.Time
	now       func() time.Time
}

// allow 判断这条进度发不发，发了就记下这次的时间
func (pt *progressThrottle) allow(p backup.Progress) bool {
	now := pt.now()
	if p.Phase != pt.lastPhase || now.Sub(pt.lastEmit) >= progressInterval {
		pt.lastPhase, pt.lastEmit = p.Phase, now
		return true
	}
	return false
}

// runJob 在自己的 goroutine 里跑作业：进度经节流发给订阅者，结束时定状态、留结果、发最后一帧
func runJob(ctx context.Context, j *job, exec func(ctx context.Context, emit func(backup.Progress)) (*Result, error)) {
	defer j.cancel()
	started := time.Now()

	throttle := progressThrottle{now: time.Now}
	emit := func(p backup.Progress) {
		jobsMu.Lock()
		j.prog = p // 查询永远拿到最新进度，节流只管发布
		send := throttle.allow(p)
		var snap Snapshot
		if send {
			snap = j.snapshotValue()
		}
		jobsMu.Unlock()
		if send {
			publish(snap)
		}
	}

	result, err := exec(ctx, emit)

	jobsMu.Lock()
	switch {
	case errors.Is(err, context.Canceled):
		j.state = StateCancelled
	case err != nil:
		j.state = StateFailed
		j.result = failedResult(err, started)
	default:
		j.state = StateDone
		result.Duration = time.Since(started)
		j.result = result
	}
	snap := j.snapshotValue()
	jobsMu.Unlock()
	publish(snap)
}

// failedResult 把失败原因整理成结果，类别码让前端不用去匹配文字
func failedResult(err error, started time.Time) *Result {
	code := int(errs.IO)
	var e *errs.Error
	if errors.As(err, &e) {
		code = int(e.Code)
	}
	return &Result{Code: code, Message: err.Error(), Duration: time.Since(started)}
}

// publish 把快照发给全部订阅者，回调在锁外调用
func publish(snap Snapshot) {
	jobsMu.Lock()
	var fns []func(Snapshot)
	for _, fn := range subs {
		fns = append(fns, fn)
	}
	jobsMu.Unlock()
	for _, fn := range fns {
		fn(snap)
	}
}

// GetJob 查一个作业的当前状态，作业号不存在报错
func GetJob(id string) (Snapshot, error) {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	j := jobs[id]
	if j == nil {
		return Snapshot{}, errs.Newf(errs.InvalidArgument, "job %q not found", id)
	}
	return j.snapshotValue(), nil
}

// Snapshots 返回全部作业的快照，按启动顺序排好，界面重载后靠它恢复
func Snapshots() []Snapshot {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	out := make([]Snapshot, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, j.snapshotValue())
	}
	sort.Slice(out, func(i, k int) bool {
		a, _ := strconv.Atoi(strings.TrimPrefix(out[i].ID, "job-"))
		b, _ := strconv.Atoi(strings.TrimPrefix(out[k].ID, "job-"))
		return a < b
	})
	return out
}

// Subscribe 订阅作业事件，之后发布的每个快照都会回调 fn；回调不许阻塞，返回退订函数
func Subscribe(fn func(Snapshot)) func() {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	nextSubID++
	id := nextSubID
	subs[id] = fn
	return func() {
		jobsMu.Lock()
		defer jobsMu.Unlock()
		delete(subs, id)
	}
}

// Cancel 请作业停下来；作业号不存在报错，已经结束的作业无事可做
func Cancel(id string) error {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	j := jobs[id]
	if j == nil {
		return errs.Newf(errs.InvalidArgument, "job %q not found", id)
	}
	j.cancel()
	return nil
}
