package upload

import (
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"

	"github.com/gin-gonic/gin"
)

const BusinessRequestLimit int64 = 32 << 20

// One parser/writer at a time bounds temporary disk use. Other HTTP and PDF
// requests do not share this slot. Never queue bodies in memory while waiting.
var multipartSlot = make(chan struct{}, 1)

func ReadForm(c *gin.Context, requestLimit int64, fileLimit int, fileBytes int64) (*multipart.Form, func(), error) {
	empty := func() {}
	select {
	case multipartSlot <- struct{}{}:
	default:
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": 503, "message": "另一文件上传正在处理，请稍后重试"})
		return nil, empty, fmt.Errorf("upload slot busy")
	}
	cleanup := func() {
		if c.Request.MultipartForm != nil {
			_ = c.Request.MultipartForm.RemoveAll()
		}
		<-multipartSlot
	}
	fail := func(err error, status int, message string) (*multipart.Form, func(), error) {
		cleanup()
		c.AbortWithStatusJSON(status, gin.H{"code": status, "message": message})
		return nil, empty, err
	}
	if c.Request.ContentLength > requestLimit {
		return fail(fmt.Errorf("request too large"), 413, "上传请求总量超过限制")
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, requestLimit)
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			return fail(err, 413, "上传请求总量超过限制")
		}
		return fail(err, 400, "文件上传不完整或表单无效")
	}
	form := c.Request.MultipartForm
	if form == nil {
		return fail(fmt.Errorf("missing multipart form"), 400, "请上传文件表单")
	}
	count := 0
	for _, files := range form.File {
		count += len(files)
		for _, file := range files {
			if file.Size <= 0 || file.Size > fileBytes {
				return fail(fmt.Errorf("invalid file size"), 400, "单文件为空或超过大小限制")
			}
		}
	}
	if count > fileLimit {
		return fail(fmt.Errorf("too many files"), 400, "上传文件数量超过限制")
	}
	fields := 0
	for key, values := range form.Value {
		fields += len(key)
		for _, value := range values {
			fields += len(value)
		}
	}
	if fields > 64<<10 {
		return fail(fmt.Errorf("form fields too large"), 413, "表单文字超过64KB")
	}
	return form, cleanup, nil
}

func ReadFile(c *gin.Context, key string, requestLimit int64, fileBytes int64) (*multipart.FileHeader, func(), error) {
	form, cleanup, err := ReadForm(c, requestLimit, 1, fileBytes)
	if err != nil {
		return nil, cleanup, err
	}
	files := form.File[key]
	if len(files) != 1 {
		cleanup()
		return nil, func() {}, fmt.Errorf("missing file field")
	}
	return files[0], cleanup, nil
}
