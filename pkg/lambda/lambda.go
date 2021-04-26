package lambda

import (
	"github.com/applike/gosoline/pkg/cfg"
	"github.com/applike/gosoline/pkg/clock"
	"github.com/applike/gosoline/pkg/mon"
	"github.com/applike/gosoline/pkg/stream"
	awsLambda "github.com/aws/aws-lambda-go/lambda"
	"os"
	"strings"
)

type Handler func(config cfg.Config, logger mon.Logger) interface{}

func Start(handler Handler, defaultConfig ...map[string]interface{}) {
	clock.WithUseUTC(true)

	// configure logger
	loggerOptions := []mon.LoggerOption{
		mon.WithFormat(mon.FormatConsole),
		// logs for lambda functions already provide timestamps, so we don't need these
		mon.WithTimestampFormat(""),
		mon.WithContextFieldsResolver(mon.ContextLoggerFieldsResolver),
	}

	logger := mon.NewLogger()
	if err := logger.Option(loggerOptions...); err != nil {
		logger.Error(err, "failed to apply logger options")
		os.Exit(1)
	}

	// configure and create config
	configOptions := []cfg.Option{
		cfg.WithEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_")),
		cfg.WithSanitizers(cfg.TimeSanitizer),
		cfg.WithErrorHandlers(func(err error, msg string, args ...interface{}) {
			logger.Error(err, msg, args...)
			os.Exit(1)
		}),
	}

	for _, defaults := range defaultConfig {
		configOptions = append(configOptions, cfg.WithConfigMap(defaults))
	}

	config := cfg.New()
	if err := config.Option(configOptions...); err != nil {
		logger.Error(err, "failed to apply logger options")
		os.Exit(1)
	}

	stream.AddDefaultEncodeHandler(mon.NewMessageWithLoggingFieldsEncoder(config, logger))

	// create handler function and give lambda control
	lambdaHandler := handler(config, logger)
	awsLambda.Start(lambdaHandler)
}
