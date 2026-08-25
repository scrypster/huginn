#!/bin/sh
# Stand-in for the `claude` CLI: reports is_error:true in its result line but
# still exits 0, the way a real run that failed mid-task but terminated
# cleanly does. Used to test that IsError is read from the stream, not just
# from the process exit code.
echo '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Trying."}]}}'
echo '{"type":"result","is_error":true,"result":"could not complete the task","total_cost_usd":0.001,"duration_ms":100,"num_turns":1,"session_id":"FAKE-SESSION"}'
exit 0
