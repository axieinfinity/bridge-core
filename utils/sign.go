package utils

import (
	"crypto/ecdsa"

	kms "github.com/axieinfinity/ronin-kms-client"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

type SignMethodConfig struct {
	PlainPrivateKey string         `json:"plainPrivateKey,omitempty"`
	KmsConfig       *kms.KmsConfig `json:"kmsConfig,omitempty"`
}

func NewSignMethod(config *SignMethodConfig) (ISign, error) {
	if config.PlainPrivateKey != "" {
		return NewPrivateKeySign(config.PlainPrivateKey)
	} else if config.KmsConfig != nil {
		return NewKmsSign(config.KmsConfig)
	}

	log.Warn("No sign methods provided")
	return nil, nil
}

type ISign interface {
	// sign function receives raw message, not hash of message
	Sign(message []byte, dataType string) ([]byte, error)
	GetAddress() common.Address
}

type PrivateKeySign struct {
	privateKey *ecdsa.PrivateKey
}

func NewPrivateKeySign(plainPrivateKey string) (*PrivateKeySign, error) {
	privateKey, err := crypto.HexToECDSA(plainPrivateKey)
	if err != nil {
		log.Error("[NewPrivateKeySign] error while getting plain private key", "err", err)
		return nil, err
	}

	return &PrivateKeySign{
		privateKey: privateKey,
	}, nil
}

type PrivateKeyConfig struct {
	PrivateKey string `json:"privateKey"`
}

func (privateKeySign *PrivateKeySign) Sign(message []byte, dataType string) ([]byte, error) {
	return crypto.Sign(crypto.Keccak256(message), privateKeySign.privateKey)
}

func (privateKeySign *PrivateKeySign) GetAddress() common.Address {
	return crypto.PubkeyToAddress(privateKeySign.privateKey.PublicKey)
}

type KmsSign struct {
	*kms.KmsSign
}

func NewKmsSign(kmsConfig *kms.KmsConfig) (*KmsSign, error) {
	kms, err := kms.NewKmsSign(kmsConfig)
	if err != nil {
		return nil, err
	}
	return &KmsSign{
		KmsSign: kms,
	}, nil
}

func (kmsSign *KmsSign) Sign(message []byte, dataType string) ([]byte, error) {
	return kmsSign.KmsSign.Sign(message, dataType)
}

func (kmsSign *KmsSign) GetAddress() common.Address {
	return kmsSign.KmsSign.Address
}
