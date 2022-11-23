package utils

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"golang.org/x/crypto/sha3"
	"math/big"
	"os"
	"reflect"
	"sync"

	kmsUtils "github.com/axieinfinity/ronin-kms-client/utils"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/signer/core"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// hasherPool holds LegacyKeccak256 hashers for rlpHash.
var hasherPool = sync.Pool{
	New: func() interface{} { return sha3.NewLegacyKeccak256() },
}

type EthClient interface {
	ethereum.ChainReader
	ethereum.TransactionReader
	ethereum.ChainStateReader
	ethereum.ContractCaller
	ethereum.LogFilterer

	BlockNumber(ctx context.Context) (uint64, error)
	ChainID(ctx context.Context) (*big.Int, error)
	Close()
}

type Utils interface {
	Invoke(any interface{}, name string, args ...interface{}) (reflect.Value, error)
	LoadAbi(path string) (*abi.ABI, error)
	GetArguments(a abi.ABI, name string, data []byte, isInput bool) (abi.Arguments, error)
	UnpackToInterface(a abi.ABI, name string, data []byte, isInput bool, v interface{}) error
	Title(text string) string
	NewEthClient(url string) (EthClient, error)
	SendContractTransaction(signMethod ISign, chainId *big.Int, fn func(opts *bind.TransactOpts) (*types.Transaction, error)) (*types.Transaction, error)
	SignTypedData(typedData core.TypedData, signMethod ISign) (hexutil.Bytes, error)
	FilterLogs(client EthClient, opts *bind.FilterOpts, contractAddresses []common.Address, filteredMethods map[*abi.ABI]map[string]struct{}) ([]types.Log, error)
	RlpHash(x interface{}) (h common.Hash)
	UnpackLog(smcAbi abi.ABI, out interface{}, event string, data []byte) error
}

type utils struct{}

func NewUtils() Utils {
	return &utils{}
}

func (u *utils) Invoke(any interface{}, name string, args ...interface{}) (reflect.Value, error) {
	method := reflect.ValueOf(any).MethodByName(name)
	methodType := method.Type()
	numIn := methodType.NumIn()
	if numIn > len(args) {
		return reflect.ValueOf(nil), fmt.Errorf("method %s must have minimum %d params. Have %d", name, numIn, len(args))
	}
	if numIn != len(args) && !methodType.IsVariadic() {
		return reflect.ValueOf(nil), fmt.Errorf("method %s must have %d params. Have %d", name, numIn, len(args))
	}
	in := make([]reflect.Value, len(args))
	for i := 0; i < len(args); i++ {
		var inType reflect.Type
		if methodType.IsVariadic() && i >= numIn-1 {
			inType = methodType.In(numIn - 1).Elem()
		} else {
			inType = methodType.In(i)
		}
		argValue := reflect.ValueOf(args[i])
		if !argValue.IsValid() {
			return reflect.ValueOf(nil), fmt.Errorf("Method %s. Param[%d] must be %s. Have %s", name, i, inType, argValue.String())
		}
		argType := argValue.Type()
		if argType.ConvertibleTo(inType) {
			in[i] = argValue.Convert(inType)
		} else {
			return reflect.ValueOf(nil), fmt.Errorf("Method %s. Param[%d] must be %s. Have %s", name, i, inType, argType)
		}
	}
	return method.Call(in)[0], nil
}

func (u *utils) LoadAbi(path string) (*abi.ABI, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	a, err := abi.JSON(f)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (u *utils) GetArguments(a abi.ABI, name string, data []byte, isInput bool) (abi.Arguments, error) {
	// since there can't be naming collisions with contracts and events,
	// we need to decide whether we're calling a method or an event
	var args abi.Arguments
	if method, ok := a.Methods[name]; ok {
		if len(data)%32 != 0 {
			return nil, fmt.Errorf("abi: improperly formatted output: %s - Bytes: [%+v]", string(data), data)
		}
		if isInput {
			args = method.Inputs
		} else {
			args = method.Outputs
		}
	}
	if event, ok := a.Events[name]; ok {
		args = event.Inputs
	}
	if args == nil {
		return nil, errors.New("abi: could not locate named method or event")
	}
	return args, nil
}

func (u *utils) UnpackToInterface(a abi.ABI, name string, data []byte, isInput bool, v interface{}) error {
	args, err := u.GetArguments(a, name, data, isInput)
	if err != nil {
		return err
	}
	unpacked, err := args.Unpack(data)
	if err != nil {
		return err
	}
	return args.Copy(v, unpacked)
}

func (u *utils) Title(text string) string {
	c := cases.Title(language.English)
	return c.String(text)
}

func (u *utils) NewEthClient(url string) (EthClient, error) {
	return ethclient.Dial(url)
}

func newKeyedTransactorWithChainID(signMethod ISign, chainID *big.Int) (*bind.TransactOpts, error) {
	if signMethod == nil {
		return nil, errors.New("no sign method found")
	}
	keyAddr := signMethod.GetAddress()
	signer := types.LatestSignerForChainID(chainID)
	return &bind.TransactOpts{
		From: keyAddr,
		Signer: func(address common.Address, tx *types.Transaction) (*types.Transaction, error) {
			if address != keyAddr {
				return nil, bind.ErrNotAuthorized
			}
			encodedTx, err := kmsUtils.RlpEncode(tx, chainID)
			if err != nil {
				return nil, err
			}
			signature, err := signMethod.Sign(encodedTx, "non-ether")
			if err != nil {
				return nil, err
			}
			return tx.WithSignature(signer, signature)
		},
		Context: context.Background(),
	}, nil
}

func (u *utils) SendContractTransaction(signMethod ISign, chainId *big.Int, fn func(opts *bind.TransactOpts) (*types.Transaction, error)) (*types.Transaction, error) {
	opts, err := newKeyedTransactorWithChainID(signMethod, chainId)
	if err != nil {
		return nil, err
	}
	return fn(opts)
}

// SignTypedData signs EIP-712 conformant typed data
// hash = keccak256("\x19${byteVersion}${domainSeparator}${hashStruct(message)}")
// It returns
// - the signature,
// - and/or any error
func (u *utils) SignTypedData(typedData core.TypedData, signMethod ISign) (hexutil.Bytes, error) {
	return u.signTypedData(typedData, signMethod)
}

// signTypedData is identical to the capitalized version
func (u *utils) signTypedData(typedData core.TypedData, signMethod ISign) (hexutil.Bytes, error) {
	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		return nil, err
	}
	typedDataHash, err := typedData.HashStruct(typedData.PrimaryType, typedData.Message)
	if err != nil {
		return nil, err
	}
	rawData := []byte(fmt.Sprintf("\x19\x01%s%s", string(domainSeparator), string(typedDataHash)))
	signature, err := signMethod.Sign(rawData, "non-ether")
	if err != nil {
		return nil, err
	}
	signature[64] += 27 // Transform V from 0/1 to 27/28 according to the yellow paper
	return signature, nil
}

func (u *utils) FilterLogs(client EthClient, opts *bind.FilterOpts, contractAddresses []common.Address, filteredMethods map[*abi.ABI]map[string]struct{}) ([]types.Log, error) {
	// Don't crash on a lazy user
	if opts == nil {
		opts = new(bind.FilterOpts)
	}
	var (
		query  [][]interface{}
		events []interface{}
	)

	for contractAbi, methods := range filteredMethods {
		for method, _ := range methods {
			if _, ok := contractAbi.Events[method]; ok {
				events = append(events, contractAbi.Events[method].ID)
			}
		}
	}
	query = append(query, events)
	topics, err := abi.MakeTopics(query...)
	if err != nil {
		return nil, err
	}
	config := ethereum.FilterQuery{
		Addresses: contractAddresses,
		Topics:    topics,
		FromBlock: new(big.Int).SetUint64(opts.Start),
	}
	if opts.End != nil {
		config.ToBlock = new(big.Int).SetUint64(*opts.End)
	}
	return client.FilterLogs(opts.Context, config)
}

func (u *utils) RlpHash(x interface{}) (h common.Hash) {
	sha := hasherPool.Get().(crypto.KeccakState)
	defer hasherPool.Put(sha)
	sha.Reset()
	rlp.Encode(sha, x)
	sha.Read(h[:])
	return h
}

func (u *utils) UnpackLog(smcAbi abi.ABI, out interface{}, event string, data []byte) error {
	var log types.Log
	if err := json.Unmarshal(data, &log); err != nil {
		return err
	}
	if log.Topics[0] != smcAbi.Events[event].ID {
		return fmt.Errorf("event signature mismatch")
	}
	if len(log.Data) > 0 {
		if err := smcAbi.UnpackIntoInterface(out, event, log.Data); err != nil {
			return err
		}
	}
	var indexed abi.Arguments
	for _, arg := range smcAbi.Events[event].Inputs {
		if arg.Indexed {
			indexed = append(indexed, arg)
		}
	}
	return abi.ParseTopics(out, indexed, log.Topics[1:])
}
