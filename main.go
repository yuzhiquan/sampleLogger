/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"github.com/go-logr/logr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
	"time"
)

const (
	jsonLogFormat = "json"
)

var timeNow = time.Now
type noopInfoLogger struct{}

func (l *noopInfoLogger) Enabled() bool                   { return false }
func (l *noopInfoLogger) Info(_ string, _ ...interface{}) {}

var disabledInfoLogger = &noopInfoLogger{}

type infoLogger struct {
	lvl zapcore.Level
	l   *zap.Logger
}

// implement logr.InfoLogger
var _ logr.InfoLogger = &infoLogger{}

// Enabled always return true
func (l *infoLogger) Enabled() bool { return true }

// Info write message to error level log
func (l *infoLogger) Info(msg string, keysAndVals ...interface{}) {
	if checkedEntry := l.l.Check(l.lvl, msg); checkedEntry != nil {
		checkedEntry.Time = timeNow()
		checkedEntry.Write(handleFields(l.l, keysAndVals)...)
	}
}

// handleFields converts a bunch of arbitrary key-value pairs into Zap fields.  It takes
// additional pre-converted Zap fields, for use with automatically attached fields, like
// `error`.
func handleFields(l *zap.Logger, args []interface{}, additional ...zap.Field) []zap.Field {
	// a slightly modified version of zap.SugaredLogger.sweetenFields
	if len(args) == 0 {
		// fast-return if we have no suggared fields.
		return additional
	}

	// unlike Zap, we can be pretty sure users aren't passing structured
	// fields (since logr has no concept of that), so guess that we need a
	// little less space.
	fields := make([]zap.Field, 0, len(args)/2+len(additional))
	for i := 0; i < len(args)-1; i += 2 {
		// check just in case for strongly-typed Zap fields, which is illegal (since
		// it breaks implementation agnosticism), so we can give a better error message.
		if _, ok := args[i].(zap.Field); ok {
			l.DPanic("strongly-typed Zap Field passed to logr", zap.Any("zap field", args[i]))
			break
		}

		// process a key-value pair,
		// ensuring that the key is a string
		key, val := args[i], args[i+1]
		keyStr, isString := key.(string)
		if !isString {
			// if the key isn't a string, DPanic and stop logging
			l.DPanic("non-string key argument passed to logging, ignoring all later arguments", zap.Any("invalid key", key))
			break
		}

		fields = append(fields, zap.Any(keyStr, val))
	}

	return append(fields, additional...)
}

// zapLogger is a logr.Logger that uses Zap to log.
type zapLogger struct {
	// NB: this looks very similar to zap.SugaredLogger, but
	// deals with our desire to have multiple verbosity levels.
	l *zap.Logger
	infoLogger
}

// implement logr.Logger
var _ logr.Logger = &zapLogger{}

// Error write log message to error level
func (l *zapLogger) Error(err error, msg string, keysAndVals ...interface{}) {
	entry := zapcore.Entry{
		Level:   zapcore.ErrorLevel,
		Time:    timeNow(),
		Message: msg,
	}
	checkedEntry := l.l.Core().Check(entry, nil)
	checkedEntry.Write(handleFields(l.l, keysAndVals, handleError(err))...)
}

// V return info logr.Logger with specified level
func (l *zapLogger) V(level int) logr.InfoLogger {
	lvl := zapcore.Level(-1 * level)
	if l.l.Core().Enabled(lvl) {
		return &infoLogger{
			l:   l.l,
			lvl: lvl,
		}
	}
	return disabledInfoLogger
}

// WithValues return logr.Logger with some keys And Values
func (l *zapLogger) WithValues(keysAndValues ...interface{}) logr.Logger {
	l.l = l.l.With(handleFields(l.l, keysAndValues)...)
	return l
}

// WithName return logger Named with specified name
func (l *zapLogger) WithName(name string) logr.Logger {
	l.l = l.l.Named(name)
	return l
}

// encoderConfig config zap json encoder key format, and encodetime format
var encoderConfig = zapcore.EncoderConfig{
	MessageKey: "msg",

	LevelKey:    "v",
	EncodeLevel: int8LevelEncoder,

	TimeKey:    "ts",
	EncodeTime: zapcore.EpochMillisTimeEncoder,
}

// NewJSONLogger creates a new json logr.Logger using the given Zap Logger to log.
func NewJSONLogger(l *zap.Logger, w zapcore.WriteSyncer) logr.Logger {
	var syncer zapcore.WriteSyncer
	syncer = os.Stdout
	if w != nil {
		syncer = w
	}
	log := l.WithOptions(zap.AddCallerSkip(1),
		zap.WrapCore(
			func(zapcore.Core) zapcore.Core {
				return zapcore.NewCore(zapcore.NewJSONEncoder(encoderConfig), zapcore.AddSync(syncer), zapcore.DebugLevel)
			}))

	return &zapLogger{
		l: log,
		infoLogger: infoLogger{
			l:   log,
			lvl: zap.DebugLevel,
		},
	}
}

func int8LevelEncoder(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	lvl := int8(l)
	if lvl < 0 {
		lvl = -lvl
	}
	enc.AppendInt8(lvl)
}

func handleError(err error) zap.Field {
	return zap.NamedError("err", err)
}

func main() {
	logger, _ := zap.NewProduction()
	l := NewJSONLogger(logger, nil)
	l = l.WithName("MyName").WithValues("user", "you")
	err := fmt.Errorf("timeout")
	l.Error(err, "Failed to update pod status")
	l.V(3).Info("test", "ns", "default", "podnum", 2)
}

// result in
//{"v":2,"ts":1590842136270.3757,"msg":"Failed to update pod status","err":"timeout"}
//{"v":1,"ts":1590842136270.4011,"msg":"hahaha"}
