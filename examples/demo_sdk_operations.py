#!/usr/bin/env python3
"""
SDK Operations Demo - Create features, tracks, and spikes for dashboard monitoring
"""

import time
from datetime import datetime

from wipnote import SDK

# Initialize SDK
sdk = SDK(agent="demo-agent")

# Track events with timestamps
events = []


def log_event(event_type: str, details: str):
    """Log event with timestamp"""
    timestamp = datetime.now().isoformat()
    event_entry = {"timestamp": timestamp, "type": event_type, "details": details}
    events.append(event_entry)
    print(f"[{timestamp}] {event_type}: {details}")


# ============================================================================
# STEP 1: Create a track
# ============================================================================
print("\n" + "=" * 80)
print("STEP 1: Creating Track 'Agent Orchestration Demo'")
print("=" * 80)

track = sdk.tracks.create("Agent Orchestration Demo").priority("high").save()

log_event("TRACK_CREATED", f"Track created: {track.id} - 'Agent Orchestration Demo'")
time.sleep(2)  # Wait for event to propagate

# ============================================================================
# STEP 2: Create 5 features linked to the track
# ============================================================================
print("\n" + "=" * 80)
print("STEP 2: Creating 5 Features linked to track")
print("=" * 80)

features = []
for i in range(1, 6):
    feature = sdk.features.create(f"Demo Feature {i}")
    feature.set_track(track.id)
    feature.add_steps(
        [
            "Step 1: Setup component",
            "Step 2: Implement functionality",
            "Step 3: Run tests",
        ]
    )
    feature.save()

    features.append(feature)
    log_event("FEATURE_CREATED", f"Feature {i} created: {feature.id}")

    # Space out feature creation events
    time.sleep(2)

# ============================================================================
# STEP 3: Update feature statuses to simulate progress
# ============================================================================
print("\n" + "=" * 80)
print("STEP 3: Updating Feature Statuses")
print("=" * 80)

# Update Feature 1 status to done
features[0].status = "done"
sdk.features._ensure_graph().update(features[0])
log_event("FEATURE_STATUS_CHANGED", f"Feature 1 ({features[0].id}) marked as DONE")
time.sleep(2)

# Update Feature 2 status to in-progress
features[1].status = "in-progress"
sdk.features._ensure_graph().update(features[1])
log_event(
    "FEATURE_STATUS_CHANGED", f"Feature 2 ({features[1].id}) marked as IN_PROGRESS"
)
time.sleep(2)

# Update Feature 3 status to in-progress
features[2].status = "in-progress"
sdk.features._ensure_graph().update(features[2])
log_event(
    "FEATURE_STATUS_CHANGED", f"Feature 3 ({features[2].id}) marked as IN_PROGRESS"
)
time.sleep(2)

# Update Feature 4 status to done
features[3].status = "done"
sdk.features._ensure_graph().update(features[3])
log_event("FEATURE_STATUS_CHANGED", f"Feature 4 ({features[3].id}) marked as DONE")
time.sleep(2)

# Feature 5 stays pending (todo status)
log_event("FEATURE_STATUS_UNCHANGED", f"Feature 5 ({features[4].id}) remaining PENDING")
time.sleep(2)

# ============================================================================
# STEP 4: Create spikes for investigation
# ============================================================================
print("\n" + "=" * 80)
print("STEP 4: Creating Spikes")
print("=" * 80)

spike1 = sdk.spikes.create("Investigate Performance")
spike1.set_findings(
    "Performance is excellent with WebSocket updates - achieved 50ms latency"
)
spike1.save()
log_event("SPIKE_CREATED", f"Spike 1 created: {spike1.id} - 'Investigate Performance'")
time.sleep(2)

spike2 = sdk.spikes.create("Research Dashboard Integration")
spike2.set_findings(
    "Dashboard integration pattern verified - real-time event streaming works"
)
spike2.save()
log_event(
    "SPIKE_CREATED", f"Spike 2 created: {spike2.id} - 'Research Dashboard Integration'"
)
time.sleep(2)

spike3 = sdk.spikes.create("Evaluate Scalability")
spike3.set_findings("System handles 1000+ concurrent connections with graph operations")
spike3.save()
log_event("SPIKE_CREATED", f"Spike 3 created: {spike3.id} - 'Evaluate Scalability'")
time.sleep(2)

# ============================================================================
# SUMMARY
# ============================================================================
print("\n" + "=" * 80)
print("SDK OPERATIONS COMPLETED")
print("=" * 80)

print(f"\nTotal Events Generated: {len(events)}")
print("\nEvent Timeline:")
print("-" * 80)

for i, event in enumerate(events, 1):
    print(f"{i:2d}. [{event['timestamp']}] {event['type']:30s} - {event['details']}")

print("\n" + "-" * 80)
print("Expected Dashboard Activity Feed Updates:")
print("  - All events should appear on the dashboard within 1-2 seconds")
print("  - Events are ordered by creation timestamp")
print("  - Feature/Track/Spike relationships are automatically tracked")
print("=" * 80)

# Additional summary statistics
print("\nSummary Statistics:")
print(f"  Track created: 1 (ID: {track.id})")
print(f"  Features created: 5 (IDs: {[f.id for f in features]})")
print("  Features Done: 2 (Features 1, 4)")
print("  Features In Progress: 2 (Features 2, 3)")
print("  Features Pending: 1 (Feature 5)")
print(f"  Spikes created: 3 (IDs: {[spike1.id, spike2.id, spike3.id]})")
print(f"  Total operations: {len(events)}")
