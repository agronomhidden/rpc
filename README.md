(This fork implements batched query in JsonRpc spec V2 based on Gorilla's rpc module. Check out the folder named "v2_batch". You should be able to enable the batched query support by changing the import path from "gorilla/rpc/v2" to "agronomhidden/rpc/v2_batch" in your code.)

rpc
===
[![Build Status](https://travis-ci.org/gorilla/rpc.png?branch=master)](https://travis-ci.org/gorilla/rpc)

gorilla/rpc is a foundation for RPC over HTTP services, providing access to the exported methods of an object through HTTP requests.

Read the full documentation here: http://www.gorillatoolkit.org/pkg/rpc
