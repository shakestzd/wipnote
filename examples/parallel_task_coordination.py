"""
Example: Parallel Task Coordination with Task IDs

Demonstrates spawning multiple tasks in parallel and retrieving
each result independently using the Task ID pattern.
"""

from wipnote import SDK
from wipnote.orchestration import delegate_with_id, get_results_by_task_id


def main():
    sdk = SDK(agent="orchestrator")

    print("🚀 Parallel Task Coordination Example\n")

    # Define 3 independent tasks
    tasks_to_delegate = [
        {
            "description": "Analyze codebase structure",
            "prompt": "List all Python files in src/ and count lines of code",
        },
        {
            "description": "Check code quality",
            "prompt": "Run ruff check and report any warnings",
        },
        {
            "description": "Review documentation",
            "prompt": "List all .md files and check for broken links",
        },
    ]

    # Generate task IDs and prepare prompts
    task_ids = []
    for task in tasks_to_delegate:
        task_id, enhanced_prompt = delegate_with_id(
            task["description"], task["prompt"], "general-purpose"
        )
        task_ids.append(task_id)

        print(f"📋 Created task {task_id}: {task['description']}")

        # In real usage, orchestrator would call:
        # Task(prompt=enhanced_prompt, description=f"{task_id}: {task['description']}")

    print("\n⏳ Waiting for results...\n")

    # Retrieve results independently
    for i, task_id in enumerate(task_ids):
        results = get_results_by_task_id(sdk, task_id, timeout=120)

        if results["success"]:
            print(f"✅ Task {i + 1} ({task_id}):")
            print(f"   Spike: {results['spike_id']}")
            print(f"   Findings: {results['findings'][:100]}...")
        else:
            print(f"❌ Task {i + 1} ({task_id}):")
            print(f"   Error: {results['error']}")
        print()

    print("✅ All tasks completed!")


if __name__ == "__main__":
    main()
