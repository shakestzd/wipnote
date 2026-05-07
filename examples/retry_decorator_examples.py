"""Comprehensive examples demonstrating the retry decorator with exponential backoff.

This file showcases various use cases and patterns for the @retry decorator,
including basic usage, custom parameters, and integration with real-world scenarios.

Run with: uv run python examples/retry_decorator_examples.py
"""

import asyncio
import logging
import time
from typing import Any

from wipnote import retry, retry_async

# Configure logging to see retry messages
logging.basicConfig(
    level=logging.DEBUG,
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
)
logger = logging.getLogger(__name__)


# ============================================================================
# Example 1: Basic API Call with Default Retry Logic
# ============================================================================


@retry()
def fetch_from_api() -> dict[str, Any]:
    """Fetch data from an API with default retry parameters.

    Uses default retry behavior:
    - 3 maximum attempts
    - 1 second initial delay
    - 2x exponential backoff
    - Jitter enabled (prevents thundering herd)
    - Retries on any Exception
    """
    logger.info("Attempting API call...")
    # Simulate occasional API failures
    if time.time() % 3 < 1:
        raise ConnectionError("API temporarily unavailable")
    return {"status": "success", "data": ["item1", "item2"]}


# ============================================================================
# Example 2: Database Connection with Specific Exception Handling
# ============================================================================


class DatabaseConnectionError(Exception):
    """Raised when database connection fails."""

    pass


@retry(
    max_attempts=5,
    initial_delay=0.5,
    exceptions=(DatabaseConnectionError, TimeoutError),
)
def connect_to_database(db_url: str) -> str:
    """Connect to database with retry only on connection errors.

    This approach:
    - Retries only on DatabaseConnectionError and TimeoutError
    - Fails immediately on other exceptions (e.g., auth errors)
    - Attempts connection up to 5 times
    - Initial delay is 0.5 seconds, growing exponentially

    Args:
        db_url: Database connection URL

    Returns:
        Connection string on success

    Raises:
        RetryError: If all 5 attempts fail
        Other exceptions: (e.g., auth errors) propagate immediately
    """
    logger.info(f"Attempting database connection to {db_url}...")
    import random

    if random.random() < 0.4:  # 40% failure rate
        raise DatabaseConnectionError("Connection refused")
    return f"Connected to {db_url}"


# ============================================================================
# Example 3: File Operations with Custom Backoff Parameters
# ============================================================================


@retry(
    max_attempts=3,
    initial_delay=0.1,
    max_delay=2.0,
    exponential_base=1.5,
    jitter=True,
)
def read_file_with_lock(filepath: str) -> str:
    """Read file, retrying if locked by another process.

    Backoff progression with exponential_base=1.5:
    - 1st retry: 0.1s delay
    - 2nd retry: 0.15s delay
    - 3rd retry: 0.225s delay

    Args:
        filepath: Path to file to read

    Returns:
        File contents on success
    """
    logger.info(f"Reading file: {filepath}...")
    # Simulate file lock scenario
    if time.time() % 2 < 0.5:
        raise OSError("File is locked")
    try:
        with open(filepath) as f:
            return f.read()
    except FileNotFoundError:
        # Don't retry on file not found (application error)
        raise


# ============================================================================
# Example 4: Custom Retry Callback for Detailed Logging
# ============================================================================


def log_retry_attempt(attempt: int, exc: Exception, delay: float) -> None:
    """Custom callback for detailed retry logging.

    Args:
        attempt: Attempt number (1-based)
        exc: Exception that triggered the retry
        delay: Delay in seconds before next attempt
    """
    logger.warning(
        f"Retry #{attempt} after {delay:.2f}s | Error: {type(exc).__name__}: {exc}"
    )


@retry(
    max_attempts=4,
    initial_delay=0.5,
    on_retry=log_retry_attempt,
    exceptions=(ConnectionError, TimeoutError),
)
def fetch_with_detailed_logging(url: str) -> dict[str, Any]:
    """Fetch data with detailed retry logging.

    The on_retry callback receives:
    - Attempt number (for counting)
    - Exception details (for diagnosis)
    - Calculated delay (for transparency)

    Args:
        url: URL to fetch from

    Returns:
        Response data on success
    """
    logger.info(f"Fetching from {url}...")
    import random

    if random.random() < 0.6:
        raise ConnectionError(f"Cannot reach {url}")
    return {"success": True}


# ============================================================================
# Example 5: Class Method with Retry
# ============================================================================


class APIClient:
    """HTTP client with automatic retry logic for resilience."""

    def __init__(self, base_url: str):
        self.base_url = base_url
        self.request_count = 0

    @retry(
        max_attempts=3,
        initial_delay=0.2,
        exceptions=(ConnectionError, TimeoutError),
    )
    def get(self, endpoint: str) -> dict[str, Any]:
        """GET request with automatic retries.

        Args:
            endpoint: API endpoint path

            Returns:
                Response data

        Raises:
            RetryError: If all 3 attempts fail
        """
        self.request_count += 1
        url = f"{self.base_url}/{endpoint}"
        logger.info(f"GET {url} (attempt #{self.request_count})")

        import random

        if random.random() < 0.5:
            raise ConnectionError(f"Connection failed to {url}")

        return {"endpoint": endpoint, "data": "response"}


# ============================================================================
# Example 6: Async Function with Retry
# ============================================================================


@retry_async(
    max_attempts=3,
    initial_delay=0.1,
    max_delay=1.0,
    exceptions=(asyncio.TimeoutError, ConnectionError),
)
async def async_fetch_data(url: str) -> dict[str, Any]:
    """Async function with retry (non-blocking).

    Uses asyncio.sleep instead of time.sleep, making it suitable for
    concurrent operations and event loop integration.

    Args:
        url: URL to fetch from

    Returns:
        Response data on success

    Raises:
        RetryError: If all attempts fail
    """
    logger.info(f"Async fetch from {url}...")
    await asyncio.sleep(0.1)  # Simulate async operation

    import random

    if random.random() < 0.4:
        raise ConnectionError(f"Cannot reach {url}")

    return {"url": url, "data": "async_response"}


# ============================================================================
# Example 7: Multiple Async Operations with Retry
# ============================================================================


async def parallel_async_operations() -> list[dict[str, Any] | BaseException]:
    """Execute multiple async operations with retry in parallel.

    Demonstrates how retry_async integrates with asyncio.gather
    for concurrent operations.

    Returns:
        List of responses from all operations
    """
    tasks = [
        async_fetch_data("https://api1.example.com"),
        async_fetch_data("https://api2.example.com"),
        async_fetch_data("https://api3.example.com"),
    ]
    results = await asyncio.gather(*tasks, return_exceptions=True)
    return results


# ============================================================================
# Example 8: Aggressive Retry for Critical Operations
# ============================================================================


@retry(
    max_attempts=10,
    initial_delay=0.05,
    max_delay=30.0,
    exponential_base=2.0,
    jitter=True,
)
def critical_operation() -> str:
    """Critical operation with aggressive retry strategy.

    This uses:
    - 10 attempts (many opportunities to succeed)
    - 0.05s initial delay (fail fast initially)
    - Exponential backoff caps at 30s (reasonable upper bound)
    - Jitter to spread retries in distributed systems

    Backoff sequence: 0.05s, 0.1s, 0.2s, 0.4s, 0.8s, 1.6s, 3.2s, 6.4s, 12.8s, 30s

    Returns:
        Result on success
    """
    logger.info("Executing critical operation...")
    import random

    if random.random() < 0.7:  # High initial failure rate
        raise RuntimeError("Critical operation failed")
    return "Critical operation succeeded"


# ============================================================================
# Example 9: No Jitter for Predictable Testing
# ============================================================================


@retry(
    max_attempts=3,
    initial_delay=0.1,
    exponential_base=2.0,
    jitter=False,  # Deterministic delays for testing
)
def predictable_retry() -> str:
    """Function with deterministic retry timing (no jitter).

    Without jitter, delays are exactly:
    - 1st retry: 0.1s
    - 2nd retry: 0.2s
    - 3rd retry: 0.4s

    Useful for testing and benchmarking where predictability matters.

    Returns:
        Result on success
    """
    logger.info("Running with predictable retry timing...")
    import random

    if random.random() < 0.6:
        raise ValueError("Predictable test error")
    return "Success with predictable timing"


# ============================================================================
# Example 10: Combining Multiple Decorators
# ============================================================================


def cache_decorator(
    func: Any,
) -> Any:
    """Simple cache decorator for demonstration."""
    cache: dict[tuple[Any, ...], Any] = {}

    def wrapper(*args: Any, **kwargs: Any) -> Any:
        key = (args, tuple(sorted(kwargs.items())))
        if key not in cache:
            cache[key] = func(*args, **kwargs)
        return cache[key]

    return wrapper


@cache_decorator
@retry(max_attempts=3, initial_delay=0.1)
def cached_api_call(user_id: int) -> dict[str, Any]:
    """API call with both retry and caching.

    Decorator order matters:
    - @cache_decorator (outer): Return cached result if available
    - @retry (inner): Retry network calls (called by cache_decorator)

    Args:
        user_id: User ID to fetch

    Returns:
        User data
    """
    logger.info(f"Fetching user {user_id}...")
    import random

    if random.random() < 0.4:
        raise ConnectionError("API connection failed")
    return {"id": user_id, "name": f"User {user_id}"}


# ============================================================================
# Main Demo
# ============================================================================


def main() -> None:
    """Run all examples to demonstrate retry decorator usage."""
    logger.info("=" * 70)
    logger.info("RETRY DECORATOR EXAMPLES")
    logger.info("=" * 70)

    # Example 1: Basic API call
    logger.info("\n1. Basic API call with default retry:")
    try:
        result = fetch_from_api()
        logger.info(f"   Result: {result}")
    except Exception as e:
        logger.error(f"   Failed: {e}")

    # Example 2: Database with specific exceptions
    logger.info("\n2. Database connection with exception filtering:")
    try:
        result = connect_to_database("postgresql://localhost/mydb")
        logger.info(f"   Result: {result}")
    except Exception as e:
        logger.error(f"   Failed: {e}")

    # Example 3: File operations with custom backoff
    logger.info("\n3. File read with custom backoff parameters:")
    try:
        # Create a test file
        test_file = "/tmp/test_retry_example.txt"
        with open(test_file, "w") as f:
            f.write("Test content")

        result = read_file_with_lock(test_file)
        logger.info(f"   File read successfully (length: {len(result)} chars)")
    except Exception as e:
        logger.error(f"   Failed: {e}")

    # Example 4: Custom logging callback
    logger.info("\n4. API fetch with detailed retry logging:")
    try:
        result = fetch_with_detailed_logging("https://api.example.com")
        logger.info(f"   Result: {result}")
    except Exception as e:
        logger.error(f"   Failed: {e}")

    # Example 5: Class method retry
    logger.info("\n5. HTTP client class with retry on methods:")
    try:
        client = APIClient("https://api.example.com")
        result = client.get("users")
        logger.info(f"   Result: {result}")
    except Exception as e:
        logger.error(f"   Failed: {e}")

    # Example 6: Async function
    logger.info("\n6. Async function with retry:")

    async def run_async_example() -> None:
        try:
            result = await async_fetch_data("https://async-api.example.com")
            logger.info(f"   Async Result: {result}")
        except Exception as e:
            logger.error(f"   Async Failed: {e}")

    asyncio.run(run_async_example())

    # Example 7: Parallel async operations
    logger.info("\n7. Parallel async operations with retry:")

    async def run_parallel_example() -> None:
        try:
            results = await parallel_async_operations()
            logger.info(f"   Parallel Results: {len(results)} operations completed")
        except Exception as e:
            logger.error(f"   Parallel Failed: {e}")

    asyncio.run(run_parallel_example())

    # Example 8: Aggressive retry
    logger.info("\n8. Critical operation with aggressive retry:")
    try:
        result = critical_operation()
        logger.info(f"   Result: {result}")
    except Exception as e:
        logger.error(f"   Failed: {e}")

    # Example 9: Predictable timing
    logger.info("\n9. Function with predictable retry timing:")
    try:
        result = predictable_retry()
        logger.info(f"   Result: {result}")
    except Exception as e:
        logger.error(f"   Failed: {e}")

    # Example 10: Cached API call
    logger.info("\n10. Cached API call (retry + caching):")
    try:
        # First call - goes to API with retry
        result1 = cached_api_call(123)
        logger.info(f"   First call: {result1}")

        # Second call - returns from cache
        result2 = cached_api_call(123)
        logger.info(f"   Second call (cached): {result2}")
    except Exception as e:
        logger.error(f"   Failed: {e}")

    logger.info("\n" + "=" * 70)
    logger.info("EXAMPLES COMPLETE")
    logger.info("=" * 70)


if __name__ == "__main__":
    main()
