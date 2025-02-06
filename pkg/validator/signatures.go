package validator

import (
	"archive/zip"
	"crypto/ed25519"
	"github.com/TicketsBot/export/internal/utils"
	"io"
	"strconv"
)

func (v *Validator) validateSignature(zipReader *zip.Reader, fileName string, data []byte) (int64, error) {
	f, err := zipReader.Open(fileName + ".sig")
	if err != nil {
		return 0, err
	}

	signature, err := io.ReadAll(v.newLimitReader(f))
	if err != nil {
		return 0, err
	}

	decoded, err := utils.Base64Decode(string(signature))
	if err != nil {
		return 0, err
	}

	if !ed25519.Verify(v.publicKey, data, decoded) {
		return 0, ErrValidationFailed
	}

	return int64(len(signature)), nil
}

func (v *Validator) validateTranscriptSignature(
	zipReader *zip.Reader,
	fileName string,
	guildId uint64,
	ticketId int,
	data []byte,
) (int64, error) {
	guildIdStr := strconv.FormatUint(guildId, 10)
	ticketIdStr := strconv.Itoa(ticketId)

	sigData := make([]byte, 0, len(guildIdStr)+len(ticketIdStr)+len(data)+2)
	sigData = append(sigData, guildIdStr...)
	sigData = append(sigData, '|')
	sigData = append(sigData, ticketIdStr...)
	sigData = append(sigData, '|')
	sigData = append(sigData, data...)

	return v.validateSignature(zipReader, fileName, sigData)
}
