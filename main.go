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
)

const (
	jsonLogFormat = "json"
)

type noopInfoLogger struct{}

func (l *noopInfoLogger) Enabled() bool                   { return false }
func (l *noopInfoLogger) Info(_ string, _ ...interface{}) {}

var disabledInfoLogger = &noopInfoLogger{}

type infoLogger struct {
	lvl zapcore.Level
	l   *zap.Logger
}

func (l *infoLogger) Enabled() bool { return true }
func (l *infoLogger) Info(msg string, keysAndVals ...interface{}) {
	if checkedEntry := l.l.Check(l.lvl, msg); checkedEntry != nil {
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
	for i := 0; i < len(args); {
		// check just in case for strongly-typed Zap fields, which is illegal (since
		// it breaks implementation agnosticism), so we can give a better error message.
		if _, ok := args[i].(zap.Field); ok {
			l.DPanic("strongly-typed Zap Field passed to logr", zap.Any("zap field", args[i]))
			break
		}

		// make sure this isn't a mismatched key
		if i == len(args)-1 {
			l.DPanic("odd number of arguments passed as key-value pairs for logging", zap.Any("ignored key", args[i]))
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
		i += 2
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

func (l *zapLogger) Error(err error, msg string, keysAndVals ...interface{}) {
	if checkedEntry := l.l.Check(zap.ErrorLevel, msg); checkedEntry != nil {
		checkedEntry.Write(handleFields(l.l, keysAndVals, handleError(err))...)
	}
}

func (l *zapLogger) V(level int) logr.InfoLogger {
	lvl := zapcore.Level(-1 * level)
	if l.l.Core().Enabled(lvl) {
		return &infoLogger{
			lvl: lvl,
			l:   l.l,
		}
	}
	return disabledInfoLogger
}

func (l *zapLogger) WithValues(keysAndValues ...interface{}) logr.Logger {
	newLogger := l.l.With(handleFields(l.l, keysAndValues)...)
	return NewJsonLogger(newLogger)
}

func (l *zapLogger) WithName(name string) logr.Logger {
	newLogger := l.l.Named(name)
	return NewJsonLogger(newLogger)
}

// NewLogger creates a new logr.Logger using the given Zap Logger to log.
func NewJsonLogger(l *zap.Logger) logr.Logger {
	cfg := zap.Config{
		Encoding: "json",
		Level:    zap.NewAtomicLevelAt(zapcore.DebugLevel),
		EncoderConfig: zapcore.EncoderConfig{
			MessageKey: "msg",

			LevelKey:    "v",
			EncodeLevel: CapitalLevelEncoder,

			TimeKey:    "ts",
			EncodeTime: zapcore.EpochMillisTimeEncoder,
		},
	}
	log := l.WithOptions(zap.AddCallerSkip(1),
		zap.WrapCore(
			func(zapcore.Core) zapcore.Core {
				return zapcore.NewCore(zapcore.NewJSONEncoder(cfg.EncoderConfig), zapcore.AddSync(os.Stdout), zapcore.DebugLevel)
			}))
	return &zapLogger{
		l: log,
		infoLogger: infoLogger{
			l:   log,
			lvl: zap.DebugLevel,
		},
	}
}

func CapitalLevelEncoder(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
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
	l := NewJsonLogger(zap.NewExample())
	l = l.WithName("MyName").WithValues("user", "you")
	err := fmt.Errorf("timeout")
	l.Error(err, "Failed to update pod status")
	l.V(1).Info("hahaha")
}

// result in
//{"v":2,"ts":1590842136270.3757,"msg":"Failed to update pod status","err":"timeout"}
//{"v":1,"ts":1590842136270.4011,"msg":"hahaha"}
