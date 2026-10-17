import base64
import json
import struct

import pytest

from runtime_agent.jupyter import decode_message


def test_default_binary_protocol_preserves_buffers():
    content = {"header": {"msg_type": "display_data"}, "content": {"data": {"text/plain": "x"}}}
    parts = [json.dumps(content).encode(), b"\x00\xff", b""]
    offsets = [4 * (len(parts) + 1)]
    for part in parts[:-1]:
        offsets.append(offsets[-1] + len(part))
    data = struct.pack("!4I", len(parts), *offsets) + b"".join(parts)
    assert decode_message(data) == {
        **content,
        "buffers_base64": [base64.b64encode(parts[1]).decode(), ""],
    }
    assert decode_message(json.dumps(content)) == content


@pytest.mark.parametrize("frame", [b"", b"00000000", struct.pack("!2I", 1, 99999)])
def test_invalid_binary_frames(frame):
    with pytest.raises(ValueError):
        decode_message(frame)
