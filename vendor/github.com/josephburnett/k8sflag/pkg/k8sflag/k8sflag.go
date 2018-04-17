package k8sflag

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Option int

const (
	Verbose  Option = iota
	Required Option = iota
	Dynamic  Option = iota
)

type flag interface {
	name() string
	set([]byte) error
	setDefault()
	isRequired() bool
}

type FlagSet struct {
	path    []string
	watcher *fsnotify.Watcher
	watches map[string]flag
	verbose bool
}

var defaultFlagSet = NewFlagSet("/etc/config")

func NewFlagSet(path string, options ...Option) *FlagSet {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}
	c := &FlagSet{
		path:    filepath.SplitList(path),
		watcher: w,
		watches: make(map[string]flag),
	}
	c.watcher.Add(path) // Watch for new files
	go func() {
		defer w.Close()
		for {
			select {
			case event := <-c.watcher.Events:
				f, ok := c.watches[event.Name]
				if !ok {
					c.verboseLog("No binding for %v.", event.Name)
					continue
				}
				c.setFromFile(f, event.Name)
			case err := <-c.watcher.Errors:
				c.verboseLog("Error event: %v", err)
			}
		}
	}()
	return c
}

func (c *FlagSet) register(f flag, options ...Option) {
	filename := filepath.Join(append(c.path, filepath.SplitList(f.name())...)...)
	if _, ok := c.watches[filename]; ok {
		panic("Flag already bound to " + f.name())
	}
	c.setFromFile(f, filename)
	if hasOption(Dynamic, options) {
		c.watches[filename] = f
		c.watcher.Add(filename)
	}
}

func (c *FlagSet) setFromFile(f flag, filename string) {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		if f.isRequired() {
			panic(fmt.Sprintf("Flag %v is required.", f.name()))
		}
		if !os.IsNotExist(err) {
			c.verboseLog("Error reading file: %v", err)
		}
		f.setDefault()
		return
	}
	err = f.set(b)
	if err != nil {
		if f.isRequired() {
			panic(fmt.Sprintf("Error reading %v: %v", f.name(), err))
		}
		f.setDefault()
	}
}

func (c *FlagSet) verboseLog(msg string, params ...interface{}) {
	if c.verbose {
		log.Printf(msg, params...)
	}
}

func info(msg string, params ...interface{}) {
	log.Printf(msg, params...)
}

type flagCommon struct {
	key      string
	value    atomic.Value
	required bool
	verbose  bool
}

func (f *flagCommon) name() string {
	return f.key
}

func (f *flagCommon) isRequired() bool {
	return f.required
}

func (f *flagCommon) verboseLog(msg string, params ...interface{}) {
	if f.verbose {
		log.Printf(msg, params...)
	}
}

type StringFlag struct {
	flagCommon
	def string
}

type BoolFlag struct {
	flagCommon
	def bool
}

type Int32Flag struct {
	flagCommon
	def int32
}

type DurationFlag struct {
	flagCommon
	def *time.Duration
}

func (s *FlagSet) String(key string, def string, options ...Option) *StringFlag {
	f := &StringFlag{}
	f.key = key
	f.verbose = s.verbose
	f.def = def
	if hasOption(Required, options) {
		f.required = true
	}
	s.register(flag(f), options...)
	return f
}

func (s *FlagSet) Bool(key string, def bool, options ...Option) *BoolFlag {
	f := &BoolFlag{}
	f.key = key
	f.def = def
	f.verbose = s.verbose
	if hasOption(Required, options) {
		f.required = true
	}
	s.register(flag(f), options...)
	return f
}

func (s *FlagSet) Int32(key string, def int32, options ...Option) *Int32Flag {
	f := &Int32Flag{}
	f.key = key
	f.def = def
	f.verbose = s.verbose
	if hasOption(Required, options) {
		f.required = true
	}
	s.register(flag(f), options...)
	return f
}

func (s *FlagSet) Duration(key string, def *time.Duration, options ...Option) *DurationFlag {
	f := &DurationFlag{}
	f.key = key
	f.def = def
	f.verbose = s.verbose
	if hasOption(Required, options) {
		f.required = true
	}
	s.register(flag(f), options...)
	return f
}

func String(key, def string, options ...Option) *StringFlag {
	return defaultFlagSet.String(key, def, options...)
}

func Bool(key string, def bool, options ...Option) *BoolFlag {
	return defaultFlagSet.Bool(key, def, options...)
}

func Int32(key string, def int32, options ...Option) *Int32Flag {
	return defaultFlagSet.Int32(key, def, options...)
}

func Duration(key string, def *time.Duration, options ...Option) *DurationFlag {
	return defaultFlagSet.Duration(key, def, options...)
}

func (f *StringFlag) set(b []byte) error {
	s := string(b)
	f.value.Store(s)
	info("Set StringFlag %v: %v.", f.key, s)
	return nil
}

func (f *BoolFlag) set(bytes []byte) error {
	s := string(bytes)
	b, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}
	f.value.Store(b)
	info("Set BoolFlag %v: %v.", f.key, b)
	return nil
}

func (f *Int32Flag) set(bytes []byte) error {
	s := string(bytes)
	i, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	f.value.Store(int32(i))
	info("Set Int32Flag %v: %v.", f.key, i)
	return nil
}

func (f *DurationFlag) set(bytes []byte) error {
	s := string(bytes)
	d, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	f.value.Store(&d)
	info("Set DurationFlag %v: %v", f.key, d)
	return nil
}

func (f *StringFlag) setDefault() {
	f.value.Store(f.def)
	info("Set StringFlag %v to default: %v.", f.key, f.def)
}

func (f *BoolFlag) setDefault() {
	f.value.Store(f.def)
	info("Set BoolFlag %v to default: %v.", f.key, f.def)
}

func (f *Int32Flag) setDefault() {
	f.value.Store(f.def)
	info("Set Int32Flag %v to default: %v.", f.key, f.def)
}

func (f *DurationFlag) setDefault() {
	f.value.Store(f.def)
	info("Set DurationFlag %v to default: %v.", f.key, f.def)
}

func (f *StringFlag) Get() string {
	return f.value.Load().(string)
}

func (f *BoolFlag) Get() bool {
	return f.value.Load().(bool)
}

func (f *Int32Flag) Get() int32 {
	return f.value.Load().(int32)
}

func (f *DurationFlag) Get() *time.Duration {
	return f.value.Load().(*time.Duration)
}

func hasOption(option Option, options []Option) bool {
	for _, o := range options {
		if o == option {
			return true
		}
	}
	return false
}
