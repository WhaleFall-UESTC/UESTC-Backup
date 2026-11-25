package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"client/internal/backup"
	"client/internal/errs"
	"client/internal/tasks"
)

// runTask 分派 task 的子命令
func runTask(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageErr("task 需要一个子命令：add、list、remove、run")
	}
	switch args[0] {
	case "add":
		return runTaskAdd(args[1:])
	case "list":
		return runTaskList(args[1:])
	case "remove":
		return runTaskRemove(args[1:])
	case "run":
		return runTaskRun(ctx, args[1:])
	default:
		return usageErr("未知 task 子命令 %q", args[0])
	}
}

func runTaskAdd(args []string) error {
	flags := flag.NewFlagSet("task add", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	config := flags.String("config", "tasks.json", "任务文件")
	name := flags.String("name", "", "任务名")
	source := flags.String("source", "", "源目录")
	dest := flags.String("dest", "", "归档路径")
	algo := flags.String("algo", "lz77", "压缩算法：none、lz77、huffman")
	if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
		fmt.Println(usage)
		return nil
	} else if err != nil {
		return usageErr("%v", err)
	}
	if _, ok := algorithms[*algo]; !ok {
		return usageErr("未知算法 %q", *algo)
	}

	list, err := tasks.Load(*config)
	if err != nil {
		return err
	}
	t := tasks.Task{Name: *name, Source: *source, Destination: *dest, Algorithm: *algo}
	if list, err = tasks.Add(list, t); err != nil {
		return err
	}
	if err := tasks.Save(*config, list); err != nil {
		return err
	}
	fmt.Printf("已添加任务 %s，共 %d 条\n", t.Name, len(list))
	return nil
}

func runTaskList(args []string) error {
	flags := flag.NewFlagSet("task list", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	config := flags.String("config", "tasks.json", "任务文件")
	if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
		fmt.Println(usage)
		return nil
	} else if err != nil {
		return usageErr("%v", err)
	}

	list, err := tasks.Load(*config)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("没有任务")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	// 写入的错误到 Flush 才报出来，这里不用逐个接
	_, _ = fmt.Fprintln(w, "名称\t算法\t源目录\t归档")
	for _, t := range list {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", t.Name, t.Algorithm, t.Source, t.Destination)
	}
	return w.Flush()
}

func runTaskRemove(args []string) error {
	flags := flag.NewFlagSet("task remove", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	config := flags.String("config", "tasks.json", "任务文件")
	if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
		fmt.Println(usage)
		return nil
	} else if err != nil {
		return usageErr("%v", err)
	}
	if flags.NArg() != 1 {
		return usageErr("task remove 需要一个参数：<名称>")
	}

	list, err := tasks.Load(*config)
	if err != nil {
		return err
	}
	if list, err = tasks.Remove(list, flags.Arg(0)); err != nil {
		return err
	}
	if err := tasks.Save(*config, list); err != nil {
		return err
	}
	fmt.Printf("已删除任务 %s，还剩 %d 条\n", flags.Arg(0), len(list))
	return nil
}

func runTaskRun(ctx context.Context, args []string) error {
	c := newCommonFlags("task run")
	config := c.fs.String("config", "tasks.json", "任务文件")
	if err := c.parse(args); errors.Is(err, flag.ErrHelp) {
		return nil
	} else if err != nil {
		return err
	}
	if c.fs.NArg() != 1 {
		return usageErr("task run 需要一个参数：<名称>")
	}
	name := c.fs.Arg(0)

	list, err := tasks.Load(*config)
	if err != nil {
		return err
	}
	i := tasks.Find(list, name)
	if i < 0 {
		return errs.Newf(errs.InvalidArgument, "task %q not found", name)
	}
	t := list[i]
	if err := tasks.Validate(t); err != nil {
		return err
	}
	algo, ok := algorithms[t.Algorithm]
	if !ok {
		return errs.Newf(errs.InvalidArgument, "task %q: unknown algorithm %q", t.Name, t.Algorithm)
	}

	entries, err := backup.Scan(t.Source)
	if err != nil {
		return err
	}
	opts := backup.PackOptions{Algorithm: algo, Password: c.password, Progress: progressPrinter(c.progress)}
	skips, err := backup.Pack(ctx, t.Destination, t.Source, entries, opts)
	if err != nil {
		return err
	}
	for _, s := range skips {
		fmt.Fprintf(os.Stderr, "跳过 %s: %v\n", s.Path, s.Err)
	}
	fmt.Printf("任务 %s 已打包 %d 个节点到 %s，跳过 %d 个\n", t.Name, len(entries)-len(skips), t.Destination, len(skips))
	return nil
}
