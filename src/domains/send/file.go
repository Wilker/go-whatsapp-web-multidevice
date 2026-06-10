package send

import "mime/multipart"

type FileRequest struct {
	BaseRequest
	File     *multipart.FileHeader `json:"file" form:"file"`
	FileURL  *string               `json:"file_url" form:"file_url"`
	FilePath *string               `json:"file_path" form:"file_path"`
	Caption  string                `json:"caption" form:"caption"`
}
