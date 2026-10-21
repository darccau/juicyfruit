package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darccau/juicyfruit/internal/memory"
	"github.com/darccau/juicyfruit/internal/pkg"
	"github.com/darccau/juicyfruit/internal/source"
	"github.com/darccau/juicyfruit/internal/task"
	"github.com/darccau/juicyfruit/internal/template"
	"github.com/darccau/juicyfruit/internal/upgrade"
	"github.com/darccau/juicyfruit/internal/version"
)

const (
	ExitSuccess     = 0
	ExitUsage       = 64
	ExitUnavailable = 69
	ExitSoftware    = 70
	ExitConfig      = 78
)

type ExitError struct{ Code int }

func (e *ExitError) Error() string { return "" }

type UsageError struct{ Message string }

func (e *UsageError) Error() string { return e.Message }
func Usagef(format string, args ...any) error {
	return &UsageError{Message: fmt.Sprintf(format, args...)}
}

type Command struct {
	Name        string
	Description string
	Usage       string
	Parser      *Parser
	Children    []*Command
	Run         func(*Context, *Values) error
}

type Context struct {
	App     *App
	Log     *Logger
	Values  *Values
	Command *Command
	Path    []string
}

type App struct {
	Out         io.Writer
	Err         io.Writer
	Registry    *source.Registry
	Snapshots   *source.SnapshotCache
	Catalog     *pkg.Catalog
	Resolver    *pkg.Resolver
	Installer   pkg.Installer
	Roots       pkg.Roots
	MemoryRoots memory.Roots
	Tasks       *task.Store
	Templates   template.Roots
}

var stdinReader io.Reader = os.Stdin

func isInteractive() bool {
	file, ok := stdinReader.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func NewApp(out, errOut io.Writer) (*App, error) {
	roots, err := pkg.DefaultRoots()
	if err != nil {
		return nil, err
	}
	memoryRoots, err := memory.DefaultRoots()
	if err != nil {
		return nil, err
	}
	registry := source.NewRegistry(source.DefaultRegistryPath())
	catalog := pkg.NewCatalog(registry)
	return &App{
		Out: out, Err: errOut,
		Registry:    registry,
		Snapshots:   source.NewSnapshotCache(filepath.Join(fsxCacheHome(), "snapshots")),
		Catalog:     catalog,
		Resolver:    pkg.NewResolver(catalog),
		Roots:       roots,
		MemoryRoots: memoryRoots,
		Tasks:       task.NewStore(task.DefaultProjectRoot()),
		Templates:   template.DefaultRoots(),
	}, nil
}

func fsxCacheHome() string {
	if value := strings.TrimSpace(os.Getenv("JUICYFRUIT_CACHE_HOME")); value != "" {
		return value
	}
	home := os.Getenv("HOME")
	if value := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); value != "" {
		return filepath.Join(value, "juicyfruit")
	}
	return filepath.Join(home, ".cache", "juicyfruit")
}

func RootCommand() *Command {
	root := &Command{Name: "juicyfruit", Description: "Gerenciador multiplataforma de skills e ferramentas do Juicyfruit."}
	root.Children = []*Command{
		initCommand(),
		sourceCommand(),
		setupCommand(),
		packageCommand(),
		updateCommand(),
		upgradeCommand(),
		memoryCommand(),
		taskCommand(),
		sessionCommand(),
		hooksCommand(),
		runCommand(),
		contextCommand(),
		templateCommand(),
	}
	return root
}

func Run(args []string, out, errOut io.Writer) int {
	logger := &Logger{Out: out, Err: errOut}
	root := RootCommand()
	if len(args) == 0 {
		printUsage(logger, root, nil)
		return ExitSuccess
	}
	if args[0] == "-v" || args[0] == "--version" {
		logger.Info(version.Version)
		return ExitSuccess
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		printUsage(logger, root, nil)
		return ExitSuccess
	}
	verbose := false
	filtered := []string{}
	for _, arg := range args {
		if arg == "--verbose" {
			verbose = true
			continue
		}
		filtered = append(filtered, arg)
	}
	logger.Verbose = verbose
	if len(filtered) == 0 {
		printUsage(logger, root, nil)
		return ExitSuccess
	}
	command, ok := findChild(root, filtered[0])
	if !ok {
		logger.Error("Could not find a command named \"%s\".", filtered[0])
		logger.Info("")
		printUsage(logger, root, nil)
		return ExitUsage
	}
	rest := filtered[1:]
	path := []string{command.Name}
	if command.Run == nil {
		if command.Name == "init" {
			if len(rest) == 0 {
				child, _ := findChild(command, "source")
				command, path = child, append(path, "source")
			} else if child, found := findChild(command, rest[0]); found {
				command, path, rest = child, append(path, child.Name), rest[1:]
			} else {
				child, _ := findChild(command, "source")
				command, path = child, append(path, "source")
			}
		} else if len(rest) == 0 {
			printUsage(logger, command, path)
			return ExitSuccess
		} else {
			child, found := findChild(command, rest[0])
			if !found {
				logger.Error("Could not find a subcommand named \"%s\" for \"%s\".", rest[0], command.Name)
				logger.Info("")
				printUsage(logger, command, path)
				return ExitUsage
			}
			command, path, rest = child, append(path, child.Name), rest[1:]
		}
	}
	for _, arg := range rest {
		if arg == "-h" || arg == "--help" {
			printUsage(logger, command, path)
			return ExitSuccess
		}
	}
	if command.Parser == nil {
		command.Parser = NewParser()
	}
	values, err := command.Parser.Parse(rest)
	if err != nil {
		logger.Error("%v", err)
		logger.Info("")
		printUsage(logger, command, path)
		return ExitUsage
	}
	app, err := NewApp(out, errOut)
	if err != nil {
		logger.Error("%v", err)
		return ExitConfig
	}
	ctx := &Context{App: app, Log: logger, Values: values, Command: command, Path: path}
	if err := command.Run(ctx, values); err != nil {
		return classifyError(logger, err)
	}
	return ExitSuccess
}

func findChild(parent *Command, name string) (*Command, bool) {
	for _, child := range parent.Children {
		if child.Name == name {
			return child, true
		}
	}
	return nil, false
}

func classifyError(logger *Logger, err error) int {
	var exit *ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	var usage *UsageError
	if errors.As(err, &usage) {
		logger.Error("%s", usage.Message)
		return ExitUsage
	}
	var sourceErr *source.Error
	if errors.As(err, &sourceErr) {
		logger.Error("%s", sourceErr.Message)
		return ExitConfig
	}
	var pkgErr *pkg.Error
	if errors.As(err, &pkgErr) {
		logger.Error("%s", pkgErr.Message)
		return ExitConfig
	}
	var memErr *memory.Error
	if errors.As(err, &memErr) {
		logger.Error("%s", memErr.Message)
		return ExitConfig
	}
	var taskErr *task.Error
	if errors.As(err, &taskErr) {
		logger.Error("%s", taskErr.Message)
		return ExitConfig
	}
	var templateErr *template.Error
	if errors.As(err, &templateErr) {
		logger.Error("%s", templateErr.Message)
		return ExitConfig
	}
	var upgradeErr *upgrade.Error
	if errors.As(err, &upgradeErr) {
		logger.Error("%s", upgradeErr.Message)
		return ExitUnavailable
	}
	logger.Error("%v", err)
	return ExitSoftware
}

func printUsage(logger *Logger, command *Command, path []string) {
	if len(path) == 0 {
		logger.Info("%s\n\nUsage: juicyfruit <command> [arguments]\n\nGlobal options:\n  -h, --help       Print this usage information.\n  -v, --version    Print the current version.\n      --verbose    Show detailed command information.\n\nAvailable commands:", command.Description)
		children := append([]*Command{}, command.Children...)
		sort.Slice(children, func(i, j int) bool { return children[i].Name < children[j].Name })
		for _, child := range children {
			logger.Info("  %-12s %s", child.Name, child.Description)
		}
		logger.Info("\nRun \"juicyfruit help <command>\" for more information about a command.")
		return
	}
	name := "juicyfruit " + strings.Join(path, " ")
	if command.Usage != "" {
		logger.Info("Usage: %s", command.Usage)
	} else if len(command.Children) > 0 {
		logger.Info("Usage: %s <subcommand> [arguments]", name)
	} else {
		logger.Info("Usage: %s [arguments]", name)
	}
	logger.Info("")
	logger.Info("%s", command.Description)
	if len(command.Children) > 0 {
		logger.Info("\nAvailable subcommands:")
		children := append([]*Command{}, command.Children...)
		sort.Slice(children, func(i, j int) bool { return children[i].Name < children[j].Name })
		for _, child := range children {
			logger.Info("  %-14s %s", child.Name, child.Description)
		}
	}
	if command.Parser != nil && len(command.Parser.specs) > 0 {
		logger.Info("\nOptions:")
		for _, spec := range command.Parser.specs {
			if spec.Hidden {
				continue
			}
			label := "    --" + spec.Name
			if spec.Alias != "" {
				label = "  -" + spec.Alias + ", --" + spec.Name
			}
			if spec.Type != BoolFlag {
				label += " <" + spec.Name + ">"
			}
			logger.Info("%-28s %s", label, spec.Help)
		}
	}
}
