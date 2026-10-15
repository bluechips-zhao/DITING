// Package secrets protects persisted credentials with an independent local key.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

const Prefix = "diting:v1:"

type Store struct{ aead cipher.AEAD }

// CheckKey validates the disk key against the key currently held in memory.
// The challenge contains no user data and never writes to disk or the database.
func (s *Store) CheckKey(path string) error {
	stored, err := Open(path, true)
	if err != nil {
		return err
	}
	const scope = "diting-key-check-v1"
	const challenge = "credential-key-health"
	sealed, err := s.Seal(scope, challenge)
	if err != nil {
		return err
	}
	plain, err := stored.OpenValue(scope, sealed)
	if err != nil {
		return err
	}
	if plain != challenge {
		return errors.New("凭据加密密钥不匹配")
	}
	return nil
}

func Open(path string, encryptedExists bool) (*Store, error) {
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("secrets.key 必须为普通文件，不能使用链接或目录")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect credential key: %w", err)
	}
	key, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if encryptedExists {
			return nil, errors.New("加密凭据已存在但 secrets.key 缺失；请恢复原密钥，不能生成替代密钥")
		}
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, fmt.Errorf("create credential key: %w", err)
		}
		_, writeErr := file.Write(key)
		syncErr := file.Sync()
		closeErr := file.Close()
		if writeErr != nil {
			return nil, writeErr
		}
		if syncErr != nil {
			return nil, syncErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
	} else if err != nil {
		return nil, fmt.Errorf("read credential key: %w", err)
	}
	if len(key) != 32 {
		return nil, errors.New("secrets.key 长度不正确；已保留原文件，请恢复正确密钥")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Store{aead: aead}, nil
}

func (s *Store) Seal(scope, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := s.aead.Seal(nonce, nonce, []byte(value), []byte(scope))
	return Prefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (s *Store) OpenValue(scope, value string) (string, error) {
	if !strings.HasPrefix(value, Prefix) {
		return value, nil
	}
	sealed, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, Prefix))
	if err != nil || len(sealed) < s.aead.NonceSize()+s.aead.Overhead() {
		return "", errors.New("加密凭据格式损坏")
	}
	plain, err := s.aead.Open(nil, sealed[:s.aead.NonceSize()], sealed[s.aead.NonceSize():], []byte(scope))
	if err != nil {
		return "", errors.New("无法解密凭据；请确认恢复的是该数据库对应的 secrets.key")
	}
	return string(plain), nil
}
