# utxo_structure_upgrade

Download and locate the package in the "go/src/github.com/dappley/go-dappley/tool" folder.

cd into the package, then build it through "go build utxo_upgrade.go".

To run the program, put the node files you want to update the in folder and run "./utxo_upgrade -file <node file name>".
  
The original file will be updated and the older copy of the file will be saved in the "old_nodes" folder.
