package discord

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"strconv"

	"github.com/ken-kai/discord-control-experiments/internal/domain"
)

func encodeMultipart(command domain.Command, uploads []domain.Upload) (io.Reader, string, error) {
	payload, ok := commandPayload(command)
	if !ok {
		return nil, "", fmt.Errorf("multipart command has no payload")
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("encode multipart metadata: %w", err)
	}
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	contentType := multipartWriter.FormDataContentType()
	go func() {
		var writeErr error
		defer func() {
			if closeErr := multipartWriter.Close(); writeErr == nil {
				writeErr = closeErr
			}
			_ = writer.CloseWithError(writeErr)
		}()
		field, err := multipartWriter.CreateFormField("payload_json")
		if err != nil {
			writeErr = err
			return
		}
		if _, err = field.Write(payloadJSON); err != nil {
			writeErr = err
			return
		}
		for i, upload := range uploads {
			header := make(textproto.MIMEHeader)
			header.Set("Content-Disposition", `form-data; name="files[`+strconv.Itoa(i)+`]"; filename="`+escapeQuoted(normalizeFilename(upload.Filename))+`"`)
			header.Set("Content-Type", "application/octet-stream")
			part, err := multipartWriter.CreatePart(header)
			if err != nil {
				writeErr = err
				return
			}
			written, err := io.CopyN(part, upload.Reader, upload.Size)
			if err != nil || written != upload.Size {
				writeErr = fmt.Errorf("attachment size mismatch")
				return
			}
			var extra [1]byte
			n, probeErr := upload.Reader.Read(extra[:])
			if n != 0 || probeErr == nil {
				writeErr = fmt.Errorf("attachment size mismatch")
				return
			}
			if probeErr != io.EOF {
				writeErr = fmt.Errorf("read attachment: %w", probeErr)
				return
			}
		}
	}()
	return reader, contentType, nil
}
func escapeQuoted(value string) string {
	result := ""
	for _, r := range value {
		switch r {
		case '\\', '"':
			result += "\\" + string(r)
		default:
			result += string(r)
		}
	}
	return result
}
