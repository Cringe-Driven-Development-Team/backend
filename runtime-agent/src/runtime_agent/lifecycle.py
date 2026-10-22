"""Finish resource operations before propagating caller cancellation."""

import asyncio
from collections.abc import Coroutine
from typing import Any


async def complete_before_cancel[T](operation: Coroutine[Any, Any, T]) -> T:
    """Wait for an owned operation even if the caller is cancelled repeatedly.

    Keep a strong reference to the operation task and shield it from caller
    cancellation. After it finishes, propagate the deferred cancellation. If the
    operation also fails, observe its exception and retain it as the cause.

    Args:
        operation (Coroutine): Operation whose side effects must finish before
            the caller proceeds to resource cleanup or shutdown.

    Returns:
        T: Operation result when the caller was not cancelled.

    Raises:
        asyncio.CancelledError: The caller was cancelled, after the operation
            finishes, or the operation task was itself cancelled.
        Exception: The operation failed and no caller cancellation was deferred.
    """
    task = asyncio.create_task(operation)
    cancellation = None
    while True:
        try:
            result = await asyncio.shield(task)
        except asyncio.CancelledError as exc:
            if task.cancelled():
                raise
            cancellation = exc
        except Exception as exc:
            if cancellation is not None:
                raise cancellation from exc
            raise
        else:
            if cancellation is not None:
                raise cancellation
            return result
