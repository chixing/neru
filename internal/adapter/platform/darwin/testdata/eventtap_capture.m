// Exercise the actual bridge without attaching a tap to the user's keyboard.
#import "../eventtap.h"
#import "../keymap.h"

#import <CoreGraphics/CoreGraphics.h>
#import <os/lock.h>

static bool nativeEnabled;
static unsigned enableCalls;
static unsigned keyCallbacks;

static void fixtureKeyCallback(const char *key, void *info) { keyCallbacks++; }

static void fixturePortCallback(CFMachPortRef port, void *message, CFIndex size, void *info) {}

static CFMachPortRef fixtureCreate(
    CGEventTapLocation location, CGEventTapPlacement placement, CGEventTapOptions options, CGEventMask mask,
    CGEventTapCallBack callback, void *info) {
	// CGEventTapCreate returns enabled. Match that behavior with an inert port.
	nativeEnabled = true;
	CFMachPortContext context = {0, NULL, NULL, NULL, NULL};
	return CFMachPortCreate(kCFAllocatorDefault, fixturePortCallback, &context, NULL);
}

static void fixtureEnable(CFMachPortRef port, bool enabled) {
	nativeEnabled = enabled;
	if (enabled)
		enableCalls++;
}

// Layout translation is unrelated to capture intent; a single printable key
// exercises whether the production callback consumes or returns an event.
static CGKeyCode fixtureKeyNameToCode(NSString *name) { return kKeyCodeA; }

static NSString *fixtureKeyCodeToName(CGKeyCode code) { return @"A"; }

static NSString *fixtureKeyCodeToCharacter(CGKeyCode code, CGEventFlags flags) { return @"a"; }

static void fixtureSetLayoutCallback(KeymapLayoutChangeCallback callback) {}

#define CGEventTapCreate fixtureCreate
#define CGEventTapEnable fixtureEnable
#define NeruKeyNameToCode fixtureKeyNameToCode
#define NeruKeyCodeToName fixtureKeyCodeToName
#define NeruKeyCodeToCharacter fixtureKeyCodeToCharacter
#define NeruSetKeymapLayoutChangeCallback fixtureSetLayoutCallback
#include "../eventtap_darwin.m"
#undef CGEventTapCreate
#undef CGEventTapEnable
#undef NeruKeyNameToCode
#undef NeruKeyCodeToName
#undef NeruKeyCodeToCharacter
#undef NeruSetKeymapLayoutChangeCallback

static void drainMainQueue(void) {
	__block bool drained = false;
	dispatch_async(dispatch_get_main_queue(), ^{
		drained = true;
	});
	while (!drained)
		CFRunLoopRunInMode(kCFRunLoopDefaultMode, 0.01, false);
}

static void require(bool condition, const char *message) {
	if (!condition) {
		fputs(message, stderr);
		fputc('\n', stderr);
		exit(1);
	}
}

static bool deliverKey(EventTap tap) {
	CGEventRef event = CGEventCreateKeyboardEvent(NULL, kKeyCodeA, true);
	require(event != NULL, "could not construct an unposted keyboard event");
	CGEventSetFlags(event, 0);
	bool passed = eventTapCallback(NULL, kCGEventKeyDown, event, tap) == event;
	CFRelease(event);
	return passed;
}

int main(int argc, const char **argv) {
	@autoreleasepool {
		require(argc == 2, "expected one scenario");
		const char *scenario = argv[1];
		EventTap tap = NeruCreateEventTap(fixtureKeyCallback, NULL);
		require(tap != NULL, "could not create the inert tap fixture");

		if (strcmp(scenario, "creation") == 0) {
			require(!nativeEnabled, "capture must be off before creation returns or the run loop drains");
			require(deliverKey(tap), "a newly created idle tap must pass keys through");
		} else if (strcmp(scenario, "active-timeout") == 0) {
			NeruEnableEventTap(tap);
			drainMainQueue();
			require(!deliverKey(tap) && keyCallbacks == 1, "an active mode must still capture and dispatch keys");
			nativeEnabled = false;
			eventTapCallback(NULL, kCGEventTapDisabledByTimeout, NULL, tap);
			require(nativeEnabled, "a timeout must recover capture when a mode wants it");
		} else if (strcmp(scenario, "stale-timeout") == 0) {
			NeruEnableEventTap(tap);
			drainMainQueue();
			NeruDisableEventTap(tap);
			drainMainQueue();
			eventTapCallback(NULL, kCGEventTapDisabledByTimeout, NULL, tap);
			require(!nativeEnabled, "a delayed timeout must not resurrect deliberately disabled capture");
		} else if (strcmp(scenario, "disabled-key") == 0) {
			NeruDisableEventTap(tap);
			drainMainQueue();
			nativeEnabled = true;
			require(deliverKey(tap), "unexpectedly enabled idle capture must not swallow a key");
			require(!nativeEnabled, "idle drift must switch capture off again");
		} else if (strcmp(scenario, "pending-disable") == 0) {
			NeruEnableEventTap(tap);
			drainMainQueue();
			NeruDisableEventTap(tap);
			require(deliverKey(tap), "disable intent must take effect before the main queue drains");
		} else if (strcmp(scenario, "last-command") == 0) {
			NeruEnableEventTap(tap);
			NeruDisableEventTap(tap);
			NeruEnableEventTap(tap);
			NeruDisableEventTap(tap);
			enableCalls = 0;
			drainMainQueue();
			require(!nativeEnabled && enableCalls == 0, "queued state changes must apply the latest intent");
		} else if (strcmp(scenario, "destroy-timeout") == 0) {
			NeruEnableEventTap(tap);
			drainMainQueue();
			NeruDestroyEventTap(tap);
			nativeEnabled = false;
			eventTapCallback(NULL, kCGEventTapDisabledByTimeout, NULL, tap);
			require(!nativeEnabled, "timeout delivery before queued destruction must not restore capture");
			drainMainQueue();
			require(gEventTapContext == NULL, "queued teardown must release its context");
			return 0;
		} else if (strcmp(scenario, "user-disabled") == 0) {
			NeruEnableEventTap(tap);
			drainMainQueue();
			nativeEnabled = false;
			eventTapCallback(NULL, kCGEventTapDisabledByUserInput, NULL, tap);
			require(!nativeEnabled, "user-disabled capture must not be automatically re-enabled");
		} else {
			require(false, "unknown scenario");
		}

		NeruDestroyEventTap(tap);
		drainMainQueue();
	}
	return 0;
}
