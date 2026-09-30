.pragma library

// Provider cursors belong to one account scope and one connection. A late
// response may fill the cache, but must not replace the current UI state.
function create(request, state, refreshed) {
    var generation = 0, account = "", active = false;
    var next = null, loading = false, warning = "", unsupported = false;
    function publish() {
        state(loading, active && (next === null || Object.keys(next).length > 0), warning, unsupported);
    }
    function load() {
        if (!active || loading || (next !== null && Object.keys(next).length === 0))
            return;
        loading = true;
        warning = "";
        publish();
        var current = generation, params = {};
        if (account !== "") params.account = account;
        if (next !== null) params.next = next;
        request(params, function(response) {
            if (current !== generation) return;
            loading = false;
            if (response.error) {
                warning = response.error;
            } else {
                var result = response.result || {};
                next = result.next || {};
                warning = result.warning || "";
                unsupported = !result.supported && Object.keys(next).length === 0;
                refreshed();
            }
            publish();
        });
    }
    return {
        reset: function(scope, enabled) {
            generation++;
            account = scope;
            active = enabled;
            next = null;
            loading = false;
            warning = "";
            unsupported = false;
            publish();
            if (active) load();
        },
        load: load
    };
}
