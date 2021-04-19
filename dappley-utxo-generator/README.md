## Dappley UTXO Generator
The tool is used to upgrade the structure of UTXOs stored in a level db.

### Environment Requirement
Golang version >= 1.14

### Usage
Copy the entire `/dappley-utxo-generator` directory to the `tool/` directory under your local go-dappley package, that is, `$GOPATH/src/github.com/dappley/go-dappley/tool/`

### Build

```bash
cd dappley-utxo-generator
go build utxo_generator.go
```

### Run
Copy the db to be converted to /dappley-utxo-generator.

To check usage,
```bash
cd dappley-utxo-generator
./utxo_generator help
```

To convert UTXOs of a range of block height,
```bash
cd dappley-utxo-generator
./utxo_generator -file <db_to_be_converted> -start <start_height> -end <end_height>
```
Note: 
- `end_height` is optional. If it is not provided, UTXOs of all blocks from `start_height` to the tail will be converted.

- if the db is already in new version, that is, it stores UTXOs in the new structure, then the conversion tool would not do anything to the db.

- if the db stores UTXOs with the old structure and has never been converted, the `start_height` must be 0.

- if the db stores UTXOs with the old structure and has been converted up to a block height, 10 for example, then to convert the rest of the db, the `start_height` must be the next block to be converted, that is 11 in this example. 


for example,
```bash
./utxo_generator -file default.db -start 0 -end 10
./utxo_generator -file default.db -start 11
```
The first command converts blocks of height 0 to 10, and the second command converts the rest of the db.