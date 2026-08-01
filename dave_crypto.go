package dgo

// DAVE support adapted from bwmarrin/discordgo PRs #1701 and #1704.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/subtle"
	"encoding/binary"
	"fmt"

	"github.com/darui3018823/dgo/mls"
)

var errNotDAVEFrame = fmt.Errorf("not a DAVE frame")

const (
	daveTagSize              = 8
	daveKeySize              = 16
	daveExportLabel          = "Discord Secure Frames v0"
	minSupplementalBytesSize = daveTagSize + 1 + 1 + 2 // tag + nonce(min 1) + sizeB + magic = 12
)

func encryptSecureFrame(frameCipher cipher.AEAD, nonce uint32, opusData []byte) []byte {
	fullNonce := buildNonce(nonce)
	sealed := frameCipher.Seal(nil, fullNonce, opusData, nil)

	ciphertext := sealed[:len(opusData)]
	fullTag := sealed[len(opusData):]
	truncatedTag := fullTag[:daveTagSize]

	nonceBytes := encodeULEB128(nonce)

	supplementalSize := byte(daveTagSize + len(nonceBytes) + 1 + 2)

	result := make([]byte, 0, len(ciphertext)+daveTagSize+len(nonceBytes)+3)
	result = append(result, ciphertext...)
	result = append(result, truncatedTag...)
	result = append(result, nonceBytes...)
	result = append(result, supplementalSize)
	result = append(result, 0xFA, 0xFA)
	return result
}

func buildNonce(counter uint32) []byte {
	nonce := make([]byte, 12)
	binary.LittleEndian.PutUint32(nonce[8:], counter)
	return nonce
}

func encodeULEB128(value uint32) []byte {
	if value == 0 {
		return []byte{0}
	}
	var result []byte
	for value > 0 {
		b := byte(value & 0x7F)
		value >>= 7
		if value > 0 {
			b |= 0x80
		}
		result = append(result, b)
	}
	return result
}

func decodeULEB128(data []byte) (uint32, int, error) {
	if len(data) == 0 {
		return 0, 0, fmt.Errorf("empty ULEB128 value")
	}

	var result uint32
	for i, b := range data {
		if i >= 5 {
			return 0, 0, fmt.Errorf("ULEB128 value exceeds 5 bytes")
		}

		value := uint32(b & 0x7F)
		if i == 4 && value > 0x0F {
			return 0, 0, fmt.Errorf("ULEB128 value overflows uint32")
		}
		result |= value << (7 * i)

		if b&0x80 == 0 {
			if i > 0 && value == 0 {
				return 0, 0, fmt.Errorf("non-canonical ULEB128 value")
			}
			return result, i + 1, nil
		}
	}
	return 0, 0, fmt.Errorf("unterminated ULEB128 value")
}

func parseSecureFrame(data []byte) (ciphertext, truncatedTag []byte, nonce uint32, err error) {
	if len(data) < 2+1+1+daveTagSize {
		err = fmt.Errorf("secure frame too short: %d bytes", len(data))
		return
	}

	if data[len(data)-1] != 0xFA || data[len(data)-2] != 0xFA {
		err = errNotDAVEFrame
		return
	}

	supplementalSize := int(data[len(data)-3])
	supplementalStart := len(data) - supplementalSize

	if supplementalStart < 0 || supplementalSize < minSupplementalBytesSize {
		err = fmt.Errorf("invalid supplemental size %d for frame of %d bytes", supplementalSize, len(data))
		return
	}

	ciphertext = data[:supplementalStart]

	nonceBytes := data[supplementalStart+daveTagSize : len(data)-3]
	var consumed int
	nonce, consumed, err = decodeULEB128(nonceBytes)
	if err != nil {
		err = fmt.Errorf("invalid DAVE nonce: %w", err)
		return
	}
	if consumed != len(nonceBytes) {
		err = fmt.Errorf("invalid DAVE nonce: %d trailing bytes", len(nonceBytes)-consumed)
		return
	}

	truncatedTag = data[supplementalStart : supplementalStart+daveTagSize]

	return
}

func decryptSecureFrame(aesBlock cipher.Block, frameCipher cipher.AEAD, nonce uint32, ciphertext, truncatedTag []byte) ([]byte, error) {
	gcmNonce := buildNonce(nonce)

	ctrIV := make([]byte, aes.BlockSize)
	copy(ctrIV, gcmNonce)
	binary.BigEndian.PutUint32(ctrIV[12:], 2)

	plaintext := make([]byte, len(ciphertext))
	cipher.NewCTR(aesBlock, ctrIV).XORKeyStream(plaintext, ciphertext)

	sealed := frameCipher.Seal(nil, gcmNonce, plaintext, nil)
	fullTag := sealed[len(plaintext):]

	if subtle.ConstantTimeCompare(fullTag[:daveTagSize], truncatedTag) != 1 {
		return nil, fmt.Errorf("DAVE tag verification failed")
	}

	return plaintext, nil
}

func newDAVECipher(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func hashRatchetGetKey(baseSecret []byte, generation uint32) ([]byte, error) {
	secret := baseSecret
	for i := uint32(0); i < generation; i++ {
		genCtx := make([]byte, 4)
		binary.BigEndian.PutUint32(genCtx, i)
		next, err := mls.ExpandWithLabel(secret, "secret", genCtx, 32)
		if err != nil {
			return nil, err
		}
		secret = next
	}
	genCtx := make([]byte, 4)
	binary.BigEndian.PutUint32(genCtx, generation)
	return mls.ExpandWithLabel(secret, "key", genCtx, 16)
}
