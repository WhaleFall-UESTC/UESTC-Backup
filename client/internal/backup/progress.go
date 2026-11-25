package backup

// Phase 是进度事件所处的阶段
type Phase uint8

const (
	PhasePacking Phase = iota
	PhaseCompressing
	PhaseEncrypting
	PhaseVerifying
	PhaseRestoring
)

// String 是阶段的中文说法，给命令行和界面显示
func (p Phase) String() string {
	switch p {
	case PhasePacking:
		return "打包"
	case PhaseCompressing:
		return "压缩"
	case PhaseEncrypting:
		return "加密"
	case PhaseVerifying:
		return "校验"
	case PhaseRestoring:
		return "还原"
	}
	return "未知"
}

// Progress 是一次进度事件。DoneBytes 是已处理的字节，跳过和校验过的文件也算进去，
// 所以结束时 DoneFiles、DoneBytes 分别等于 TotalFiles、TotalBytes
type Progress struct {
	Phase      Phase
	Path       string // 正在处理的文件，阶段事件时为空
	DoneFiles  int
	TotalFiles int
	DoneBytes  int64
	TotalBytes int64
}

// emitPhase 发一条不带数量的阶段事件
func emitPhase(progress func(Progress), phase Phase) {
	if progress != nil {
		progress(Progress{Phase: phase})
	}
}

// countEntries 数出普通文件的个数和总字节数
func countEntries(entries []Entry) (int, int64) {
	var files int
	var bytes int64
	for _, e := range entries {
		if e.Kind == KindFile {
			files++
			bytes += e.Size
		}
	}
	return files, bytes
}
