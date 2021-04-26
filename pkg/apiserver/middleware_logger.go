package apiserver

import (
	"fmt"
	"github.com/applike/gosoline/pkg/mon"
	"github.com/gin-gonic/gin"
	"strings"
	"time"
)

func LoggingMiddleware(logger mon.Logger) gin.HandlerFunc {
	chLogger := logger.WithChannel("http")

	return func(ginCtx *gin.Context) {
		start := time.Now()

		ginCtx.Next()

		req := ginCtx.Request
		ctx := req.Context()
		log := chLogger.WithContext(ctx)

		path := req.URL.Path
		pathRaw := getPathRaw(ginCtx)

		referer := req.Referer()

		query := req.URL.Query()
		queryRaw := req.URL.RawQuery
		queryParameters := make(map[string]string)

		for k := range query {
			queryParameters[k] = query.Get(k)
		}

		method := ginCtx.Request.Method
		requestTimeNano := time.Since(start)
		requestTimeSecond := float64(requestTimeNano) / float64(time.Second)

		log = log.WithFields(mon.Fields{
			"bytes":                    ginCtx.Writer.Size(),
			"client_ip":                ginCtx.ClientIP(),
			"host":                     req.Host,
			"protocol":                 req.Proto,
			"request_method":           method,
			"request_path":             path,
			"request_path_raw":         pathRaw,
			"request_query":            queryRaw,
			"request_query_parameters": queryParameters,
			"request_referer":          referer,
			"request_time":             requestTimeSecond,
			"scheme":                   req.URL.Scheme,
			"status":                   ginCtx.Writer.Status(),
		})

		if len(ginCtx.Errors) == 0 {
			log.Info("%s %s %s", method, path, req.Proto)
			return
		}

		for _, e := range ginCtx.Errors {
			switch e.Type {
			case gin.ErrorTypeBind:
				log.Warn("%s %s %s - bind error - %v", method, path, req.Proto, e.Err)
			case gin.ErrorTypeRender:
				log.Warn("%s %s %s - render error - %v", method, path, req.Proto, e.Err)
			default:
				log.Error(e.Err, "%s %s %s", method, path, req.Proto)
			}
		}
	}
}
func getPathRaw(ginCtx *gin.Context) string {
	path := ginCtx.Request.URL.Path

	for i := range ginCtx.Params {
		p := ginCtx.Params[i]
		k := fmt.Sprintf(":%s", p.Key)
		path = strings.Replace(path, p.Value, k, 1)
	}

	return path
}
