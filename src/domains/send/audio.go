package send

import "mime/multipart"

type AudioRequest struct {
	BaseRequest
	Audio     *multipart.FileHeader `json:"audio" form:"audio"`
	AudioURL  *string               `json:"audio_url" form:"audio_url"`
	AudioPath *string               `json:"audio_path" form:"audio_path"`
	PTT       bool                  `json:"ptt" form:"ptt"`
}
