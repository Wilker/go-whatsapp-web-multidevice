package utils

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestDetermineMediaExtension(t *testing.T) {
	tests := []struct {
		name       string
		filename   string
		mimeType   string
		wantSuffix string
	}{
		{
			name:       "DocxFromFilename",
			filename:   "report.docx",
			mimeType:   "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			wantSuffix: ".docx",
		},
		{
			name:       "XlsxFromMime",
			filename:   "",
			mimeType:   "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			wantSuffix: ".xlsx",
		},
		{
			name:       "PptxFromMime",
			filename:   "",
			mimeType:   "application/vnd.openxmlformats-officedocument.presentationml.presentation",
			wantSuffix: ".pptx",
		},
		{
			name:       "ZipFallback",
			filename:   "",
			mimeType:   "application/zip",
			wantSuffix: ".zip",
		},
		{
			name:       "AudioOgaWithCodecsParam",
			filename:   "",
			mimeType:   "audio/ogg; codecs=opus",
			wantSuffix: ".oga",
		},
		{
			name:       "ExeFromFilename",
			filename:   "installer.exe",
			mimeType:   "application/octet-stream",
			wantSuffix: ".exe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determineMediaExtension(tt.filename, tt.mimeType)
			if got != tt.wantSuffix {
				t.Fatalf("determineMediaExtension() = %q, want %q", got, tt.wantSuffix)
			}
		})
	}
}

func TestExtractMediaInfoReturnsDirectPath(t *testing.T) {
	msg := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			URL:           proto.String("https://mmg.whatsapp.net/test"),
			DirectPath:    proto.String("/mms/image/test"),
			Caption:       proto.String("foto"),
			MediaKey:      []byte{1, 2, 3},
			FileSHA256:    []byte{4, 5, 6},
			FileEncSHA256: []byte{7, 8, 9},
			FileLength:    proto.Uint64(123),
		},
	}

	mediaType, filename, url, directPath, mediaKey, fileSHA256, fileEncSHA256, fileLength := ExtractMediaInfo(msg)

	if mediaType != "image" {
		t.Fatalf("expected media type image, got %q", mediaType)
	}
	if filename == "" {
		t.Fatal("expected generated filename")
	}
	if url != "https://mmg.whatsapp.net/test" {
		t.Fatalf("expected url to be preserved, got %q", url)
	}
	if directPath != "/mms/image/test" {
		t.Fatalf("expected direct path to be preserved, got %q", directPath)
	}
	if len(mediaKey) == 0 || len(fileSHA256) == 0 || len(fileEncSHA256) == 0 {
		t.Fatal("expected media hashes and key to be preserved")
	}
	if fileLength != 123 {
		t.Fatalf("expected file length 123, got %d", fileLength)
	}
}

func TestExtractReplyContextReturnsQuotedMetadata(t *testing.T) {
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("Resposta"),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:    proto.String("quoted-msg-1"),
				Participant: proto.String("5511888888888@s.whatsapp.net"),
				QuotedMessage: &waE2E.Message{
					Conversation: proto.String("Mensagem original"),
				},
			},
		},
	}

	reply := ExtractReplyContext(msg)
	if reply.RepliedID != "quoted-msg-1" {
		t.Fatalf("expected replied id quoted-msg-1, got %q", reply.RepliedID)
	}
	if reply.QuotedParticipant != "5511888888888@s.whatsapp.net" {
		t.Fatalf("expected quoted participant to be preserved, got %q", reply.QuotedParticipant)
	}
	if reply.QuotedMessage != "Mensagem original" {
		t.Fatalf("expected quoted message to be preserved, got %q", reply.QuotedMessage)
	}
}
