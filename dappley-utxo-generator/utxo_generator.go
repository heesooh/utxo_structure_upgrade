package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/dappley/go-dappley/common/hash"
	"github.com/dappley/go-dappley/core/account"
	"github.com/dappley/go-dappley/core/block"
	"github.com/dappley/go-dappley/core/utxo"
	"github.com/dappley/go-dappley/logic/lutxo"
	"github.com/dappley/go-dappley/storage"
	"github.com/dappley/go-dappley/util"
	"google.golang.org/grpc/status"
)

var (
	tipKey             = []byte("tailBlockHash")
	largeBlockNumBound = uint64(10000)
)

var (
	ErrBlockDoesNotExist    = errors.New("block does not exist in db")
	ErrTailHashDoesNotExist = errors.New("tail hash does not exist in db")
	ErrTargetHeightNotValid = errors.New("target height is not valid")
	ErrFileNotExist         = errors.New("File does not exist")
)

//command names
const (
	utxoConvert = "utxoConvert"
	utxoDelete  = "utxoDelete"
	help        = "help"
)

//flag name
const (
	flagDatabase    = "file"
	flagStartHeight = "start"
	flagEndHeight   = "end"
)

//command list
var cmdList = []string{
	utxoConvert,
	utxoDelete,
	help,
}

type valueType int

//type enum
const (
	valueTypeString = iota
	valueTypeUint64
)

type flagPars struct {
	name         string
	defaultValue interface{}
	valueType    valueType
	usage        string
}

//descryption of each function
var descrip = map[string]string{
	utxoConvert: "convert utxos from blocks from the start height to the end height including both endpoints",
	utxoDelete:  "Delete all utxos in the database",
}

//configure input parameters/flags for each command
var cmdFlagsMap = map[string][]flagPars{
	utxoConvert: {
		flagPars{
			flagDatabase,
			"default.db",
			valueTypeString,
			"database name. Eg. default.db",
		},
		flagPars{
			flagStartHeight,
			uint64(0),
			valueTypeUint64,
			"start height. Eg. 0",
		},
		flagPars{
			flagEndHeight,
			uint64(0),
			valueTypeUint64,
			"end height. Eg. 0",
		},
	},
	utxoDelete: {
		flagPars{
			flagDatabase,
			"default.db",
			valueTypeString,
			"database name. Eg. default.db",
		},
	},
}

type commandHandler func(flags cmdFlags)

//map the callback function to each command
var cmdHandlers = map[string]commandHandler{
	utxoConvert: utxoConvertCmdHandler,
	utxoDelete:  utxoDeleteCmdHandler,
	help:        helpCmdHandler,
}

//map key: flag name   map defaultValue: flag defaultValue
type cmdFlags map[string]interface{}

func main() {
	args := os.Args[1:]

	if len(args) < 1 {
		printUsage()
		return
	}
	cmdFlagSetList := map[string]*flag.FlagSet{}
	//set up flagset for each command
	for _, cmd := range cmdList {
		fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
		cmdFlagSetList[cmd] = fs
	}
	cmdFlagValues := map[string]cmdFlags{}
	//set up flags for each command
	for cmd, pars := range cmdFlagsMap {
		cmdFlagValues[cmd] = cmdFlags{}
		for _, par := range pars {
			switch par.valueType {
			case valueTypeString:
				cmdFlagValues[cmd][par.name] = cmdFlagSetList[cmd].String(par.name, par.defaultValue.(string), par.usage)
			case valueTypeUint64:
				cmdFlagValues[cmd][par.name] = cmdFlagSetList[cmd].Uint64(par.name, par.defaultValue.(uint64), par.usage)
			}
		}
	}
	cmdName := args[0]

	cmd := cmdFlagSetList[cmdName]
	if cmd == nil {
		fmt.Println("\nError:", cmdName, "is an invalid command")
		printUsage()
	} else {
		err := cmd.Parse(args[1:])
		if err != nil {
			return
		}
		if cmd.Parsed() {
			cmdHandlers[cmdName](cmdFlagValues[cmdName])
		}
	}
}

//------------------------------------core functions-------------------------------------//
func printUsage() {
	fmt.Println("Usage:")
	for _, cmd := range cmdList {
		fmt.Println(" ", cmd)
	}
	fmt.Println("Note: Use the command 'help' to get the command usage in details")
}

func utxoConvertCmdHandler(flags cmdFlags) {
	dbname := *(flags[flagDatabase].(*string))
	startHeight := *(flags[flagStartHeight].(*uint64))
	endHeight := *(flags[flagEndHeight].(*uint64))

	db, err := LoadDBFile(dbname)
	if err != nil {
		fmt.Println("Error: File does not exist!")
		return
	}
	defer db.Close()
	//check whether the start height and end height are valid
	tailBlock, err := GetTailBlock(db)
	if err != nil {
		fmt.Println("Error: fail to get tail block!")
		return
	}
	tailHeight := tailBlock.GetHeight()
	//endHeight == 0 means the tailHeight
	if endHeight == uint64(0) {
		endHeight = tailHeight
	}
	if startHeight > endHeight {
		fmt.Println("Error: start height should not be larger than the end height!")
		return
	}
	if endHeight > tailHeight {
		fmt.Println("Error: end height should not be larger than the tail height!")
		return
	}
	fmt.Printf("Current database is %s, start height = %d, end height = %d", dbname, startHeight, endHeight)

	//delete all old utxos
	keyExist, err := DeleteAllUtxosFromOldDb(db)
	if err != nil {
		return
	}
	if keyExist {
		fmt.Println("\nDelete all the old utxos in the database...")
	} else {
		fmt.Println("\nThe old utxo structure doesn't exist in the database already...")
	}

	largeSet := false
	if tailHeight >= largeBlockNumBound {
		largeSet = true
	}

	utxoCache := utxo.NewUTXOCache(db)
	utxoIndex := lutxo.NewUTXOIndex(utxoCache)
	fmt.Println("Start converting transactions in blocks...")
	for i := startHeight; i <= endHeight; i++ {
		block, err := GetBlockByHeight(db, i)
		if err != nil {
			fmt.Println("Error: fail to get block ", status.Convert(err).Message())
			return
		}
		blkTxs := block.GetTransactions()
		utxoIndex.UpdateUtxos(blkTxs)
		if largeSet {
			if i%100 == 0 {
				//print the convert info every 100 blocks
				fmt.Println("Finish converting the block of height", i)
				//PrintBlock(block)
			}
		} else {
			fmt.Println("Finish converting the block of height", i)
			//PrintBlock(block)
		}
	}
	//save the results in db
	err = utxoIndex.Save()
	if err != nil {
		fmt.Println("Error: fail to save utxoindex ", status.Convert(err).Message())
		return
	}
	fmt.Println("Finish saving...")
}

func utxoDeleteCmdHandler(flags cmdFlags) {
	dbname := *(flags[flagDatabase].(*string))
	db, err := LoadDBFile(dbname)
	if err != nil {
		fmt.Println("Error: File does not exist!")
		return
	}
	defer db.Close()
	isDeleted, err := DeleteAllUtxosFromNewDb(db)
	if err != nil {
		fmt.Println("Error: fail to delete all utxos from db!")
		return
	}
	if isDeleted {
		fmt.Println("All utxos have been already deleted!")
	}
}

func helpCmdHandler(flag cmdFlags) {
	for cmd, pars := range cmdFlagsMap {
		fmt.Println("\n-----------------------------------------------------------------")
		fmt.Printf("Command: %s\n", cmd)
		fmt.Printf("Description: %s\n", descrip[cmd])
		fmt.Printf("Usage Example: ./utxo_generator %s", cmd)
		for _, par := range pars {
			fmt.Printf(" -%s", par.name)
			if par.name == flagDatabase {
				fmt.Printf(" default.db ")
				continue
			}
			if par.name == flagStartHeight {
				fmt.Printf(" 0 ")
				continue
			}
			if par.name == flagEndHeight {
				fmt.Printf(" 10 ")
				continue
			}
		}
	}
	fmt.Println()
}

//------------------------------------------help functions--------------------------//
func isDbExist(filename string) bool {
	_, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return true
}

func LoadDBFile(filename string) (storage.Storage, error) {
	isExist := isDbExist(filename)
	if !isExist {
		return nil, ErrFileNotExist
	}
	db := storage.OpenDatabase(filename)
	return db, nil
}

func GetBlockByHeight(db storage.Storage, height uint64) (*block.Block, error) {
	h, err := db.Get(util.UintToHex(height))
	if err != nil {
		return nil, ErrBlockDoesNotExist
	}
	rawBytes, err := db.Get(h)

	return block.Deserialize(rawBytes), nil
}

func GetTailBlock(db storage.Storage) (*block.Block, error) {
	hash, err := db.Get(tipKey)
	if err != nil {
		return nil, ErrTailHashDoesNotExist
	}
	rawBytes, err := db.Get(hash)

	return block.Deserialize(rawBytes), nil
}

func DeleteAllUtxosFromOldDb(db storage.Storage) (bool, error) {
	tailBlock, err := GetTailBlock(db)
	keyExist := false
	if err != nil {
		fmt.Println("Error: fail to get tail block!")
		return false, err
	}
	tailHeight := tailBlock.GetHeight()
	for i := uint64(0); i <= tailHeight; i++ {
		block, err := GetBlockByHeight(db, i)
		if err != nil {
			fmt.Println("Error: fail to get block ", status.Convert(err).Message())
			return keyExist, err
		}
		blkTxs := block.GetTransactions()
		for _, tx := range blkTxs {
			for _, vout := range tx.Vout {
				pkh := []byte(vout.PubKeyHash)
				v, err := db.Get(pkh)
				if v != nil && err != storage.ErrKeyInvalid {
					err = db.Del(pkh)
					if err != nil {
						fmt.Println("Error: fail to delete pubkeyhash-utxotx pairs!")
						return keyExist, err
					}
					keyExist = true
				}
			}
		}
	}
	return keyExist, nil
}

func DeleteAllUtxosFromNewDb(db storage.Storage) (bool, error) {
	var pubKeySet map[string]int
	pubKeySet = make(map[string]int)
	keyId := 0
	//store all pubkeys from the blocks into map
	tailBlock, err := GetTailBlock(db)
	if err != nil {
		fmt.Println("Error: fail to get tail block!")
		return false, err
	}
	tailHeight := tailBlock.GetHeight()
	for i := uint64(0); i <= tailHeight; i++ {
		block, err := GetBlockByHeight(db, i)
		if err != nil {
			fmt.Println("Error: fail to get block ", status.Convert(err).Message())
			return false, err
		}
		blkTxs := block.GetTransactions()
		for _, tx := range blkTxs {
			for _, vout := range tx.Vout {
				pkh := vout.PubKeyHash.String()
				_, ok := pubKeySet[pkh]
				if !ok {
					pubKeySet[pkh] = keyId
					keyId++
				}
			}
		}
	}
	//remove utxotx from db
	utxoCache := utxo.NewUTXOCache(db)
	isDeleted := true
	for pubkeyStr, _ := range pubKeySet {
		pkBytes, err := hex.DecodeString(pubkeyStr)
		//first check ifthe pubkey still exist in the database
		_, err = db.Get(util.Str2bytes(pubkeyStr))
		if err != nil {
			continue
		}
		isDeleted = false
		if err != nil {
			fmt.Println("Error: fail to decode pubkey string!")
			return isDeleted, err
		}
		utxotx := utxoCache.GetUTXOTx(account.PubKeyHash(pkBytes))
		err = utxoCache.RemoveUtxos(utxotx, pubkeyStr)
		if err != nil {
			fmt.Println("Error: fail to remove utxotx!")
			return isDeleted, err
		}
		fmt.Println("Delete all utxos of pubkey", pubkeyStr)
	}
	return isDeleted, nil
}

func PrintBlock(b *block.Block) {

	encodedBlock := map[string]interface{}{
		"Header": map[string]interface{}{
			"Hash":      b.GetHash().String(),
			"Prevhash":  b.GetPrevHash().String(),
			"Timestamp": b.GetTimestamp(),
			"Producer":  b.GetProducer(),
			"height":    b.GetHeight(),
		},
		"Transactions": tx_pretty_string(b),
	}

	blockinfo, err := json.MarshalIndent(encodedBlock, "", "  ")
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	fmt.Println(string(blockinfo))
	fmt.Println("\n")
}

func tx_pretty_string(b *block.Block) []map[string]interface{} {
	var encodedTransactions []map[string]interface{}

	for _, transaction := range b.GetTransactions() {

		var encodedVin []map[string]interface{}
		for _, vin := range transaction.Vin {
			encodedVin = append(encodedVin, map[string]interface{}{
				"Vout":      vin.Vout,
				"Signature": hex.EncodeToString(vin.Signature),
				"PubKey":    hex.EncodeToString(vin.PubKey),
			})
		}

		var encodedVout []map[string]interface{}
		for _, vout := range transaction.Vout {
			encodedVout = append(encodedVout, map[string]interface{}{
				"Value":      vout.Value,
				"PubKeyHash": hex.EncodeToString(vout.PubKeyHash),
				"Contract":   vout.Contract,
			})
		}

		encodedTransaction := map[string]interface{}{
			"ID":   hash.Hash(transaction.ID).String(),
			"Vin":  encodedVin,
			"Vout": encodedVout,
		}
		encodedTransactions = append(encodedTransactions, encodedTransaction)
	}

	return encodedTransactions
}
