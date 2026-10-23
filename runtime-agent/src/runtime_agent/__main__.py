"""Start the runtime agent as a single-worker Uvicorn process."""

import os

import uvicorn


def main():
    """Apply a private file-creation mask and run the configured HTTP server.

    Raises:
        ValueError: PORT cannot be parsed as an integer.
    """
    os.umask(0o077)
    uvicorn.run(
        "runtime_agent.api:create_app",
        factory=True,
        host=os.environ.get("HOST", "127.0.0.1"),
        port=int(os.environ.get("PORT", "8000")),
        workers=1,
        timeout_graceful_shutdown=45,
        proxy_headers=False,
    )


if __name__ == "__main__":
    main()
