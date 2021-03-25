//This is a tool for converting utxo structure from v0.3.0 in the database stage

package main

import(
	"os"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"encoding/hex"
	copy "github.com/otiai10/copy"
	"github.com/golang/protobuf/proto"
	logger "github.com/sirupsen/logrus"
	"github.com/dappley/go-dappley/util"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/dappley/go-dappley/common"
	"github.com/dappley/go-dappley/storage"
	"github.com/dappley/go-dappley/core/utxo"
	"github.com/dappley/go-dappley/core/account"
	"github.com/dappley/go-dappley/core/transactionbase"
	newutxopb "github.com/dappley/go-dappley/core/utxo/pb"
	oldutxopb "github.com/dappley/go-dappley/tool/utxo_structure_upgrade/oldpb"
	v3utxopb "github.com/dappley/go-dappley/tool/utxo_structure_upgrade/pbs/v0.3.0/pb"
	v4utxopb "github.com/dappley/go-dappley/tool/utxo_structure_upgrade/pbs/v0.4.0/pb"
	// v5utxopb "github.com/dappley/go-dappley/tool/utxo_structure_upgrade/pbs/v0.5.0/pb"
)

type UtxoType int

const (
	UtxoNormal UtxoType = iota
	UtxoCreateContract
	UtxoInvokeContract
)

type OldUTXO struct {
	transactionbase.TXOutput
	Txid     []byte
	TxIndex  int
	UtxoType UtxoType
}

type UTXOTxOld struct {
	Key  []string
	UTXO []*OldUTXO
}

type UTXOTxNew struct {
	Key  []string
	UTXO []*utxo.UTXO
}

type OldUtxoIndex struct {
	PublicKey []string
	OldUTXOTx []UTXOTxOld
}

func main(){
	args := os.Args[1:]
	if (len(args) < 1 || (len(args) >= 1 && args[0] != "-file")) {
		printUsage()
		return
	}
	var filePath, version string
	flag.StringVar(&filePath, "file", "default.db", "default db file path")
	flag.StringVar(&version, "version", "v0.5.0", "version of the database file")
	flag.Parse()

	validVersion := isVersionValid(version)
	if !validVersion {
		logger.Error("Input version is invalid! Must be one of: v0.3.0, v0.4.0, v0.5.0")
		return
	}

	isFileExist := isDbExist(filePath)
	if !isFileExist {
		logger.Error("Cannot find such file in the directory!")
		return
	}

	logger.Infof("Current database name is %s [version %s]", filePath, version)

	fmt.Println("Start Converting......")

	oldUtxoIndex := getOldUtxoIndexFromDB(filePath, version)
	//printInfoOfOldUtxoIndex(oldUtxoIndex)
	ConvertAndSaveUtxoIndexToDB(filePath, oldUtxoIndex)
}

//-------------------------------core functions-------------------------------------

//get old utxoindex (key = account.PubkeyHash.String(), value = old utxotx)
func getOldUtxoIndexFromDB(dbfilename string, version string) OldUtxoIndex {
	var publicKey []string
	var oldUTXOTx []UTXOTxOld
	
	db, err := leveldb.OpenFile(dbfilename, nil)
	if err != nil {
		logger.Error("failed to open db!")
		return OldUtxoIndex {
			PublicKey: publicKey,
			OldUTXOTx: oldUTXOTx,
		}
	}
	defer db.Close()

	iter := db.NewIterator(nil, nil)
	i := 0
	for iter.Next() {
		curKey := iter.Key()
		curValue := iter.Value()
		err, utxotxold := DeserializeUTXOTx(curValue, version)
		isValidUtxo := isValidUtxoKeyValue(curKey, curValue)

		if err == nil && len(utxotxold.Key) != 0 && len(utxotxold.UTXO) != 0 && !isValidUtxo {
			if isValidUtxotx(curKey, utxotxold){
				i++
				publicKey = append(publicKey, account.PubKeyHash(curKey).String())
				oldUTXOTx = append(oldUTXOTx, utxotxold)
			}
			//utxotxold.printInfoWithPubKey(util.Bytes2str(iter.Key()))
		}
	}

	fmt.Println("The number of old utxotx is ", i)
	iter.Release()
	err = iter.Error()
	if err != nil {
		logger.Error("Iter error!")
	}

	return OldUtxoIndex {
		PublicKey: publicKey,
		OldUTXOTx: oldUTXOTx,
	}
}

//convert old utxo index and save the results in db
func ConvertAndSaveUtxoIndexToDB(dbfilename string, oldUtxoIndex OldUtxoIndex){
	publicKey := oldUtxoIndex.PublicKey
	oldUTXOTx := oldUtxoIndex.OldUTXOTx

	if (len(publicKey) != len(oldUTXOTx)) {
		err := "length of public key and old utxo transactions are different!"
		panic(err)
	}

	if(len(publicKey) == 0) {
		fmt.Println("old utxo index doesn't exist in db!")
		return
	}

	new_dbfilename := strings.TrimSuffix(dbfilename, ".db") + "_old.db"
	err := copy.Copy(dbfilename, "./old_nodes/" + new_dbfilename)
	if err != nil {
		panic(err)
	}

	db := storage.OpenDatabase(dbfilename)
	defer db.Close()
	//newUtxoCache := utxo.NewUTXOCache(db)

	utxo_converted := 0
	for i := 0; i < len(publicKey); i++ {		
		pubkey    := publicKey[i]
		oldutxotx := oldUTXOTx[i]

		newutxotx := oldutxotx.ConvertUtxotx()
		pubkeyhash, err := hex.DecodeString(pubkey)
		if err != nil {
			logger.Error("Failed to decode pubkey hash string")
			return
		}
		err = db.Del(pubkeyhash)
		if err != nil {
			logger.Error("Failed to delete pubkey-utxotx pair from db")
			return
		}
		//add new utxotx into db
		err = AddUtxos(db, newutxotx, pubkey)
		if err != nil {
			logger.WithError(err).Error("Failed to add UTXOTx into db!")
			return
		}
		utxo_converted++
	}
	fmt.Println("The number of converted utxotx is ", utxo_converted)
	return
}

//------------------------------helper functions------------------------------------

func printUsage() {
	fmt.Println("--------------------------------------------------------------------------")
	fmt.Println("Usage: upgrade the utxo structure from v0.3.0 in the database")
	fmt.Println("Usage example: ./utxo_upgrade -file default.db")
	fmt.Println("Version before update will be saved in the \"old_nodes\" folder as backup")
}

func isVersionValid(version string) bool {
	return (version == "v0.3.0" || version == "v0.4.0" || version == "v0.5.0")
}

func isDbExist(filename string) bool {
	_, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return true
}

func(outxo *OldUTXO) FromProto(pb proto.Message) {
	utxopb := pb.(*oldutxopb.Utxo)
	outxo.Value = common.NewAmountFromBytes(utxopb.Amount)
	outxo.PubKeyHash = utxopb.PublicKeyHash
	outxo.Txid = utxopb.Txid
	outxo.TxIndex = int(utxopb.TxIndex)
	outxo.UtxoType = UtxoType(utxopb.UtxoType)
	outxo.Contract = utxopb.Contract
}

func (outxo *OldUTXO) ToProto() proto.Message {
	return &oldutxopb.Utxo{
		Amount:        outxo.Value.Bytes(),
		PublicKeyHash: []byte(outxo.PubKeyHash),
		Txid:          outxo.Txid,
		TxIndex:       uint32(outxo.TxIndex),
		UtxoType:      uint32(outxo.UtxoType),
		Contract:      outxo.Contract,
	}
}

func (utxoTxOld *UTXOTxOld) PutUtxo(oldutxo *OldUTXO) {
	//key := hex.EncodeToString(oldutxo.Txid) + "-" + strconv.Itoa(oldutxo.TxIndex)
	key := string(oldutxo.Txid) + "_" + strconv.Itoa(oldutxo.TxIndex)
	utxoTxOld.Key = append(utxoTxOld.Key, key)
	utxoTxOld.UTXO = append(utxoTxOld.UTXO, oldutxo)
}

func (utxoTxNew *UTXOTxNew) PutUtxo(newutxo *utxo.UTXO) {
	key := newutxo.GetUTXOKey()
	utxoTxNew.Key  = append(utxoTxNew.Key, key)
	utxoTxNew.UTXO = append(utxoTxNew.UTXO, newutxo)
}

func DeserializeUTXOTx(d []byte, version string) (error, UTXOTxOld) {
	utxoTxOld := NewUTXOTxOld()

	if version == "v0.3.0" {
		v3utxoList := &v3utxopb.UtxoList{}
		err := proto.Unmarshal(d, v3utxoList)
		if err != nil {
			//logger.WithFields(logger.Fields{"error": err}).Error("UtxoTx: parse UtxoTx failed.")
			return err, utxoTxOld
		}
		for _, utxoPb := range v3utxoList.Utxos {
			var oldutxo = &OldUTXO{}
			oldutxo.FromProto(utxoPb)
			utxoTxOld.PutUtxo(oldutxo)
		}
	} else {
		v4utxo := &v4utxopb.Utxo{}
		err := proto.Unmarshal(d, v4utxo)
		if err != nil {
			//logger.WithFields(logger.Fields{"error": err}).Error("UtxoTx: parse UtxoTx failed.")
			return err, utxoTxOld
		}
		for v4utxo.NextUtxoKey != nil {
			var oldutxo = &OldUTXO{}
			oldutxo.FromProto(v4utxo)
			utxoTxOld.PutUtxo(oldutxo)
			err := proto.Unmarshal(v4utxo.NextUtxoKey, v4utxo)
			if err != nil {
				return err, utxoTxOld
			}
		}
	}

	return nil, utxoTxOld
}

//check if the rawbytes are new utxo-type
func isValidUtxoKeyValue(key []byte, value []byte) bool {
	var utxo = &utxo.UTXO{}
	utxoPb := &newutxopb.Utxo{}
	err := proto.Unmarshal(value, utxoPb)
	if err != nil {
			//logger.WithFields(logger.Fields{"error": err}).Error("Unmarshal utxo failed.")
			return false
	}
	utxo.FromProto(utxoPb)
	utxokey := utxo.GetUTXOKey()
	return strings.Compare(utxokey, string(key)) == 0
}

//only true for valid utxotx when each utxo in the utxotx has the same pubkey as the utxotx pubkey and txid not empty
func isValidUtxotx(key[] byte, utxotxold UTXOTxOld) bool {
	pubkey := account.PubKeyHash(key).String()
	utxotxold_key  := utxotxold.Key
	utxotxold_utxo := utxotxold.UTXO
	if (len(utxotxold_key) != len(utxotxold_utxo)) {
		err := "Length of utxotxold_key and utxotxold_utxo are different!"
		panic(err)
	}
	for i := 0; i < len(utxotxold_key); i++ {
		value := utxotxold_utxo[i]
		utxo_pubkey := hex.EncodeToString(value.PubKeyHash)
		if strings.Compare(pubkey, utxo_pubkey) != 0 {
			return false
		}
		if len(value.Txid) == 0 {
			return false
		}
	}
	return true
}

func NewUTXOTxOld() UTXOTxOld {
	return UTXOTxOld {
		Key: nil,
		UTXO: nil,
	}
}

func NewUTXOTxNew() UTXOTxNew {
	return UTXOTxNew {
		Key: nil,
		UTXO: nil,
	}
}

func(outxo *OldUTXO) ConvertUtxo() *utxo.UTXO {
	oldTxOutput := transactionbase.TXOutput{outxo.Value, outxo.PubKeyHash, outxo.Contract}
	newUTXO := utxo.NewUTXO(oldTxOutput, outxo.Txid, outxo.TxIndex, utxo.UtxoType(outxo.UtxoType))
	return newUTXO
}

//convert old utxotx to new utxotx, if old utxotx is empty, return empty new Utxotx
func (utxoTxOld UTXOTxOld) ConvertUtxotx() *UTXOTxNew {
	NewUTXOTx := NewUTXOTxNew()
	key  := utxoTxOld.Key
	utxo := utxoTxOld.UTXO

	if (len(key) != len(utxo)) {
		err := "Length of key and utxo are different!"
		panic(err)
	}
	
	end  := len(key) - 1
	for i := end; i >= 0; i-- {
		newutxo := utxo[i].ConvertUtxo()
		NewUTXOTx.PutUtxo(newutxo)
	}
	return &NewUTXOTx
}

func putUTXOToDB(db storage.Storage, utxo *utxo.UTXO) error {
	utxoBytes, err := proto.Marshal(utxo.ToProto().(*newutxopb.Utxo))
	if err != nil {
		return err
	}
	err = db.Put(util.Str2bytes(utxo.GetUTXOKey()), utxoBytes)
	if err != nil {
		logger.WithFields(logger.Fields{"error": err}).Error("put utxo to db failed！")
		return err
	}
	return nil
}

func putLastUTXOKeyToDB(db storage.Storage, pubkey string, lastUtxoKey []byte) error {
	err := db.Put(util.Str2bytes(pubkey), lastUtxoKey)
	if err != nil {
		logger.WithFields(logger.Fields{"error": err}).Error("put last utxo key to db failed.")
		return err
	}
	return nil
}

func AddUtxos(db storage.Storage, utxoTx *UTXOTxNew, pubkey string) error {
	var lastUtxoKey []byte
	key  := utxoTx.Key
	utxo := utxoTx.UTXO

	if (len(key) != len(utxo)) {
		err := "length of key and utxo in UTXOTxNew are different!"
		panic(err)
	}

	//lastUtxoKey := getLastUTXOKey(db, pubkey)

	for i := 0; i < len(key); i++ {
		KEY  := key[i]
		UTXO := utxo[i]
		UTXO.NextUtxoKey = lastUtxoKey
		err := putUTXOToDB(db, UTXO)
		if err != nil {
			return err
		}
		lastUtxoKey = util.Str2bytes(KEY)
	}

	err := putLastUTXOKeyToDB(db, pubkey, lastUtxoKey)
	if err != nil {
		return err
	}

	return nil
}

//print information of old itxo index
func printInfoOfOldUtxoIndex(oldutxoindex OldUtxoIndex) {
	for i := 0; i < len(oldutxoindex.PublicKey); i++ {
		oldutxoindex.OldUTXOTx[i].printInfoWithPubKey(oldutxoindex.PublicKey[i])
	}
}

//print info of old utxotx (here pubkey type is account.PubKeyHash.String())
func (utxoTxOld UTXOTxOld) printInfoWithPubKey(pubkey string) {
	fmt.Println("pubkey string = ", pubkey)
	fmt.Println("old utxotx details: ")
	for i := 0; i < len(utxoTxOld.Key); i++ {
		key  := utxoTxOld.Key[i]
		value := utxoTxOld.UTXO[i]
		fmt.Printf("key = %s\n", key)
		fmt.Println("utxo Value = ", value.Value)
		fmt.Println("utxo PubKeyHash = ", value.PubKeyHash)
		if (value.Contract != "") {
			fmt.Println("utxo Contract = ", value.Contract)
		}
		fmt.Println("utxo Txid = ", value.Txid)
		fmt.Println("utxo TxIndex = ", value.TxIndex)
		fmt.Println("utxo UtxoType = ", value.UtxoType, "\n")
	}
	fmt.Println("-----------------------------------------")
}

//------------------------------unused functions------------------------------------

// func (utxoTxOld UTXOTxOld) Serialize() []byte {
// 	utxoList := &oldutxopb.UtxoList{}

// 	for _, utxo := range utxoTxOld.Indices {
// 		utxoList.Utxos = append(utxoList.Utxos, utxo.ToProto().(*oldutxopb.Utxo))
// 	}
// 	bytes, err := proto.Marshal(utxoList)
// 	if err != nil {
// 		logger.WithFields(logger.Fields{"error": err}).Error("UtxoTx: serialize UtxoTx failed.")
// 		return nil
// 	}
// 	return bytes
// }

//loop over all key-value pairs in the database
// func loopAllKeyValuePairs(dbfilename string) {
// 	db, err := leveldb.OpenFile(dbfilename, nil)
// 	if err != nil {
// 		logger.Error("failed to open db!")
// 		return
// 	}
// 	iter := db.NewIterator(nil, nil)
// 	i := 0
// 	for iter.Next() {
// 		if i > 20 {
// 			break
// 		}
// 		i++
// 		fmt.Println("key = ", iter.Key())
// 		fmt.Println("value = ", iter.Value())
// 	}
// 	db.Close()
// }

// func getLastUTXOKey(db storage.Storage, pubkey string) []byte {
// 	lastUtxoKey, err := db.Get(util.Str2bytes(pubkey))
// 	if err != nil {
// 		return []byte{}
// 	}
// 	return lastUtxoKey
// }