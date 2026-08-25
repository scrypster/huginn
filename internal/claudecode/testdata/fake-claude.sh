#!/bin/sh
# Stand-in for the `claude` CLI in tests. Echoes its own argv on the first
# line so argument-construction tests can assert against it, then emits a
# canned stream-json conversation.
echo "{\"type\":\"argv\",\"argv\":\"$*\"}"
echo '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Working on it."}]}}'
echo '{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/tmp/x"}}]}}'
echo '{"type":"result","subtype":"success","result":"Done: renamed the function.","total_cost_usd":0.0123,"duration_ms":4210,"num_turns":3,"session_id":"FAKE-SESSION"}'
exit 0
