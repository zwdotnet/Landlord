package rental

import (
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type checkedUpload struct {
	file      *multipart.FileHeader
	ext, mime string
}
type storedUpload struct {
	file *multipart.FileHeader
	path string
	row  interface{}
}

func checkUploadFiles(files []*multipart.FileHeader, types map[string]string, max int64) ([]checkedUpload, error) {
	checked := make([]checkedUpload, 0, len(files))
	for _, file := range files {
		if file.Size <= 0 || file.Size > max {
			return nil, fmt.Errorf("%s：文件为空或超过大小限制", file.Filename)
		}
		src, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("读取上传文件失败")
		}
		header := make([]byte, 512)
		n, _ := src.Read(header)
		src.Close()
		mime := http.DetectContentType(header[:n])
		ext, ok := types[mime]
		if !ok {
			return nil, fmt.Errorf("%s：不支持的文件类型", file.Filename)
		}
		checked = append(checked, checkedUpload{file, ext, mime})
	}
	return checked, nil
}

// Prevalidated batches become visible together. Any disk/DB/commit failure
// rolls back the rows and removes every file written by this attempt.
func saveUploadBatch(c *gin.Context, db *gorm.DB, files []storedUpload, checkQuota func(*gorm.DB) error) error {
	committed := false
	paths := make([]string, 0, len(files))
	defer func() {
		if !committed {
			for _, path := range paths {
				_ = os.Remove(path)
			}
		}
	}()
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := checkQuota(tx); err != nil {
			return err
		}
		for _, file := range files {
			if err := os.MkdirAll(filepath.Dir(file.path), 0755); err != nil {
				return err
			}
			paths = append(paths, file.path)
			if err := c.SaveUploadedFile(file.file, file.path); err != nil {
				return err
			}
			if err := tx.Create(file.row).Error; err != nil {
				return err
			}
		}
		return nil
	})
	committed = err == nil
	return err
}
