package mon

import (
	"context"
	"fmt"
	"github.com/jonboulle/clockwork"
	"io"
	"os"
	"reflect"
	"sync"
	"time"
)

const (
	Trace = "trace"
	Debug = "debug"
	Info  = "info"
	Warn  = "warn"
	Error = "error"
)

var levels = map[string]int{
	Trace: 0,
	Debug: 1,
	Info:  2,
	Warn:  3,
	Error: 4,
}

func levelPriority(level string) int {
	return levels[level]
}

const (
	ChannelDefault   = "default"
	FormatConsole    = "console"
	FormatGelf       = "gelf"
	FormatGelfFields = "gelf_fields"
	FormatJson       = "json"
)

type Tags map[string]interface{}
type ConfigValues map[string]interface{}
type Fields map[string]interface{}
type EcsMetadata map[string]interface{}

type Metadata struct {
	Channel       string
	Context       context.Context
	ContextFields Fields
	Fields        Fields
	Tags          Tags
}

type formatter func(timestamp string, level string, msg string, err error, data *Metadata) ([]byte, error)

var formatters = map[string]formatter{
	FormatConsole:    formatterConsole,
	FormatGelf:       formatterGelf,
	FormatGelfFields: formatterGelfFields,
	FormatJson:       formatterJson,
}

type GosoLog interface {
	Logger
	Option(options ...LoggerOption) error
}

//go:generate mockery -name Logger
type Logger interface {
	Debug(format string, args ...interface{})
	Info(format string, args ...interface{})
	Warn(format string, args ...interface{})
	Error(err error, format string, args ...interface{})

	WithChannel(channel string) Logger
	WithContext(ctx context.Context) Logger
	WithFields(fields Fields) Logger
}

type logger struct {
	clock       clockwork.Clock
	output      io.Writer
	outputLck   *sync.Mutex
	ctxResolver []ContextFieldsResolver
	hooks       []LoggerHook

	level           int
	format          string
	timestampFormat string

	data Metadata
}

func NewLogger() *logger {
	return NewLoggerWithInterfaces(clockwork.NewRealClock(), os.Stdout)
}

func NewLoggerWithInterfaces(clock clockwork.Clock, out io.Writer) *logger {
	logger := &logger{
		clock:           clock,
		output:          out,
		outputLck:       &sync.Mutex{},
		ctxResolver:     make([]ContextFieldsResolver, 0),
		hooks:           make([]LoggerHook, 0),
		level:           levelPriority(Info),
		format:          FormatConsole,
		timestampFormat: "15:04:05.000",
		data: Metadata{
			Channel:       ChannelDefault,
			ContextFields: make(Fields),
			Fields:        make(Fields),
			Tags:          make(Tags),
		},
	}

	return logger
}

func (l *logger) copy() *logger {
	return &logger{
		clock:           l.clock,
		outputLck:       l.outputLck,
		output:          l.output,
		ctxResolver:     l.ctxResolver,
		hooks:           l.hooks,
		level:           l.level,
		format:          l.format,
		timestampFormat: l.timestampFormat,
		data:            l.data,
	}
}

func (l *logger) Option(options ...LoggerOption) error {
	for _, opt := range options {
		if err := opt(l); err != nil {
			return err
		}
	}

	return nil
}

func (l *logger) WithChannel(channel string) Logger {
	cpy := l.copy()
	cpy.data.Channel = channel

	return cpy
}

func (l *logger) WithContext(ctx context.Context) Logger {
	if ctx == nil {
		return l
	}

	cpy := l.copy()
	cpy.data.Context = ctx

	for _, r := range l.ctxResolver {
		newContextFields := r(ctx)
		cpy.data.ContextFields = mergeMapStringInterface(cpy.data.ContextFields, newContextFields)
	}

	return cpy
}

func (l *logger) WithFields(fields Fields) Logger {
	cpy := l.copy()
	cpy.data.Fields = mergeMapStringInterface(l.data.Fields, fields)

	return cpy
}

func (l *logger) Info(msg string, args ...interface{}) {
	l.log(Info, msg, args, nil, Fields{})
}

func (l *logger) Debug(msg string, args ...interface{}) {
	if l.level > levels[Debug] {
		return
	}

	l.log(Debug, msg, args, nil, Fields{})
}

func (l *logger) Warn(msg string, args ...interface{}) {
	l.log(Warn, msg, args, nil, Fields{})
}

func (l *logger) Error(err error, msg string, args ...interface{}) {
	l.logError(Error, err, msg, args)
}

func (l *logger) logError(level string, err error, msg string, args []interface{}) {
	l.log(level, msg, args, err, Fields{
		"stacktrace": GetStackTrace(1),
	})
}

func (l *logger) log(level string, msg string, args []interface{}, logErr error, fields Fields) {
	levelNo := levels[level]

	if levelNo < l.level {
		return
	}

	cpyData := l.data
	cpyData.Fields = mergeMapStringInterface(cpyData.Fields, fields)

	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}

	for _, h := range l.hooks {
		if err := h.Fire(level, msg, logErr, &cpyData); err != nil {
			l.err(err)
		}
	}

	timestamp := l.clock.Now().Format(l.timestampFormat)
	buffer, err := formatters[l.format](timestamp, level, msg, logErr, &cpyData)

	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to write to log, %v\n", err)
	}

	l.write(buffer)
}

func (l *logger) err(err error) {
	timestamp := l.clock.Now().Format(l.timestampFormat)
	buffer, err := formatters[l.format](timestamp, Error, err.Error(), err, &l.data)

	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to write to log, %v\n", err)
	}

	l.write(buffer)
}

func (l *logger) write(buffer []byte) {
	l.outputLck.Lock()
	defer l.outputLck.Unlock()

	_, err := l.output.Write(buffer)

	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to write to log, %v\n", err)
	}
}

func mergeMapStringInterface(receiver map[string]interface{}, input map[string]interface{}) map[string]interface{} {
	newMap := make(map[string]interface{}, len(receiver)+len(input))

	for k, v := range receiver {
		newMap[k] = prepareForLog(v)
	}

	for k, v := range input {
		newMap[k] = prepareForLog(v)
	}

	return newMap
}

func prepareForLog(v interface{}) interface{} {
	switch t := v.(type) {
	case error:
		// Otherwise errors are ignored by `encoding/json`
		return t.Error()
	case time.Time:
		return v
	case map[string]interface{}:
		// perform a deep copy of any maps contained in this map element
		// to ensure we own the object completely
		return mergeMapStringInterface(t, nil)

	default:
		// same as before, but handle the case of the map mapping to something
		// different than interface{}
		// should quite rarely get hit, otherwise you are using too complex objects for your logs
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Map:
			iter := rv.MapRange()
			newMap := make(map[string]interface{}, rv.Len())

			for iter.Next() {
				keyValue := iter.Key()
				elemValue := iter.Value()
				newMap[fmt.Sprint(keyValue.Interface())] = prepareForLog(elemValue.Interface())
			}

			return newMap

		case reflect.Ptr, reflect.Interface:
			if rv.IsNil() {
				return nil
			}

			return prepareForLog(rv.Elem().Interface())

		case reflect.Struct:
			rvt := rv.Type()
			newMap := make(map[string]interface{}, rv.NumField())

			for i := 0; i < rv.NumField(); i++ {
				field := rv.Field(i)
				if !field.CanInterface() {
					continue
				}
				newMap[rvt.Field(i).Name] = prepareForLog(field.Interface())
			}

			return newMap

		case reflect.Slice, reflect.Array:
			if rv.Kind() == reflect.Slice && rv.IsNil() {
				return nil
			}

			newArray := make([]interface{}, rv.Len())

			for i := range newArray {
				newArray[i] = prepareForLog(rv.Index(i).Interface())
			}

			return newArray

		default:
			return v
		}
	}
}
