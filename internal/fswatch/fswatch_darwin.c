//go:build darwin && cgo

#include <CoreServices/CoreServices.h>
#include <dispatch/dispatch.h>
#include "_cgo_export.h"

static void ankhFSCallback(ConstFSEventStreamRef ref, void *info, size_t n, void *paths,
                           const FSEventStreamEventFlags flags[], const FSEventStreamEventId ids[]) {
	(void)ref; (void)flags; (void)ids;
	ankhFSEvents((uintptr_t)info, n, (char **)paths);
}

typedef struct {
	FSEventStreamRef stream;
	dispatch_queue_t queue;
} ankhStream;

void *ankhFSStart(uintptr_t handle, char **paths, int n, double latency) {
	CFMutableArrayRef arr = CFArrayCreateMutable(NULL, n, &kCFTypeArrayCallBacks);
	for (int i = 0; i < n; i++) {
		CFStringRef s = CFStringCreateWithCString(NULL, paths[i], kCFStringEncodingUTF8);
		if (s != NULL) {
			CFArrayAppendValue(arr, s);
			CFRelease(s);
		}
	}
	FSEventStreamContext ctx = {0, (void *)handle, NULL, NULL, NULL};
	FSEventStreamRef stream = FSEventStreamCreate(NULL, ankhFSCallback, &ctx, arr,
		kFSEventStreamEventIdSinceNow, latency,
		kFSEventStreamCreateFlagFileEvents | kFSEventStreamCreateFlagNoDefer | kFSEventStreamCreateFlagWatchRoot);
	CFRelease(arr);
	if (stream == NULL) {
		return NULL;
	}
	dispatch_queue_t q = dispatch_queue_create("dev.ankh.fsevents", DISPATCH_QUEUE_SERIAL);
	FSEventStreamSetDispatchQueue(stream, q);
	if (!FSEventStreamStart(stream)) {
		FSEventStreamInvalidate(stream);
		FSEventStreamRelease(stream);
		dispatch_release(q);
		return NULL;
	}
	ankhStream *s = malloc(sizeof(ankhStream));
	s->stream = stream;
	s->queue = q;
	return s;
}

void ankhFSStop(void *p) {
	ankhStream *s = (ankhStream *)p;
	FSEventStreamStop(s->stream);
	FSEventStreamInvalidate(s->stream);
	// Wait for a callback that may be running on the queue to finish.
	dispatch_sync(s->queue, ^{});
	FSEventStreamRelease(s->stream);
	dispatch_release(s->queue);
	free(s);
}
