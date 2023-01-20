package main

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func isGoFile(file File) bool {
	ext := filepath.Ext(file.Name())
	return ext == ".go"
}

func isNotGoProtoFile(file File) bool {
	if strings.Index(file.Name(), ".pb.") != -1 {
		return false
	}
	return true
}

func isNotGoMockFile(file File) bool {
	if strings.Index(file.Name(), "mock/") != -1 {
		return false
	}
	return true
}

func isNotGoTestFile(file File) bool {
	if strings.Index(file.Name(), "_test.") != -1 {
		return false
	}
	return true
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filters := []Filters{
		isGoFile,
		isNotGoProtoFile,
		isNotGoMockFile,
		isNotGoTestFile,
	}

	opt := Options{}
	opt.GetCommits.Limit = 10

	files, err := getOrderFiles(getCommits, ctx, opt, filters...)
	if err != nil {
		log.Panic(err)
	}
	for _, file := range files {
		commitTime, err := file.GetCommit().CommitTime(ctx)
		if err != nil {
			log.Panic(err)
		}
		fmt.Println(commitTime, file.GetCommit().CommitHash(), file.Name())
	}
}

type Filters func(File) bool

type GetCommits func(ctx context.Context, opt Options) ([]Commit, error)

type Options struct {
	GetCommits struct {
		Limit int
	}
}

func getOrderFiles(fn GetCommits, ctx context.Context, opt Options, filters ...Filters) ([]File, error) {
	uniqueFiles := make([]File, 0)
	mapExistedFiles := make(map[string]File)

	commits, err := fn(ctx, opt)
	if err != nil {
		return nil, err
	}

	for _, commit := range commits {
		files, err := commit.GetFiles(ctx)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if _, ok := mapExistedFiles[file.Name()]; !ok {
				filterChains := true
				for _, isSatisfyFilter := range filters {
					if !isSatisfyFilter(file) {
						filterChains = false
						break
					}
				}
				if filterChains {
					mapExistedFiles[file.Name()] = file
					uniqueFiles = append(uniqueFiles, file)
				}
			}
		}
	}
	return uniqueFiles, nil
}

func getCommits(ctx context.Context, opt Options) ([]Commit, error) {
	if opt.GetCommits.Limit == 0 {
		opt.GetCommits.Limit = 1
	}

	commitHashes, err := cmdGetCommits(ctx, opt)
	if err != nil {
		return nil, err
	}

	commits := make([]Commit, 0, len(commitHashes))
	for _, commitHash := range commitHashes {
		if commitHash == "" {
			continue
		}
		commits = append(commits, NewCommit(commitHash))
	}
	return commits, nil
}

type File interface {
	Name() string
	GetCommit() Commit
}

type fileObj struct {
	Commit
	name string
}

func NewFile(c Commit, name string) File {
	return &fileObj{Commit: c, name: name}
}

func (f *fileObj) GetCommit() Commit {
	return f.Commit
}

func (f *fileObj) Name() string {
	return f.name
}

type Commit interface {
	GetFiles(context.Context) ([]File, error)
	CommitTime(context.Context) (time.Time, error)
	CommitHash() string
}

type commitObj struct {
	commitHash string
}

func NewCommit(message string) Commit {
	return &commitObj{commitHash: message}
}

func (c *commitObj) CommitHash() string {
	return c.commitHash
}

func (c *commitObj) CommitTime(ctx context.Context) (time.Time, error) {
	return cmdGetCommitTime(ctx, c.CommitHash())
}

func (c *commitObj) GetFiles(ctx context.Context) ([]File, error) {
	fileNames, err := cmdGetFiles(ctx, c.CommitHash())
	if err != nil {
		return nil, err
	}

	files := make([]File, 0, len(fileNames))
	for _, fileName := range fileNames {
		if fileName == "" {
			continue
		}
		files = append(files, NewFile(c, fileName))
	}
	return files, nil
}

var cmdCache = make(map[interface{}]interface{})

func cmdGetCommits(ctx context.Context, opt Options) ([]string, error) {
	output, err := exec.CommandContext(ctx,
		"git",
		"log",
		fmt.Sprintf("-%d", opt.GetCommits.Limit),
		"--pretty=format:%h",
	).Output()
	if err != nil {
		return nil, err
	}

	return strings.Split(string(output), "\n"), nil
}

func cmdGetFiles(ctx context.Context, commitHash string) ([]string, error) {
	output, err := exec.CommandContext(ctx,
		"git",
		"diff",
		"--name-only",
		commitHash,
	).Output()
	if err != nil {
		return nil, err
	}
	return strings.Split(string(output), "\n"), nil
}

func cmdGetCommitTime(ctx context.Context, commitHash string) (time.Time, error) {
	var output []byte

	cacheKey := fmt.Sprintf("cmdGetCommitTime-%s", commitHash)
	if result, ok := cmdCache[cacheKey]; ok {
		output, _ = result.([]byte)
	}

	if len(output) == 0 {
		out, err := exec.CommandContext(ctx,
			"git",
			"show",
			"-s",
			"--format=%cD",
			commitHash,
		).Output()
		if err != nil {
			return time.Time{}, err
		}
		output = out
		cmdCache[cacheKey] = output
	}

	parsedTime, err := time.Parse(time.RFC1123Z, string(output)[:len(output)-1])
	if err != nil {
		return time.Time{}, err
	}
	return parsedTime, nil
}
