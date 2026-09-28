.pragma library

// One list request at a time. Changing filters invalidates the active response
// immediately, including the interval before the search debounce fires.
function create(request, publish, state, failed, pageSize) {
    pageSize = pageSize || 200;
    var filters = {}, key = "", generation = 0, serial = 0;
    var inFlight = false, pendingRefresh = false, loading = false;
    var rows = [], nextOffset = 0, hasMore = false;

    function notify() { state(loading, hasMore); }

    function setFilters(value) {
        var nextKey = JSON.stringify(value);
        if (nextKey === key)
            return;
        key = nextKey;
        filters = value;
        generation++;
        // A queued event refresh must not bypass a new query's debounce.
        pendingRefresh = false;
        rows = [];
        nextOffset = 0;
        hasMore = false;
        loading = true;
        publish(rows);
        notify();
    }

    function fetchPage(offset) {
        var requestGeneration = generation, requestSerial = ++serial;
        var params = Object.assign({}, filters, { limit: pageSize + 1, offset: offset });
        inFlight = true;
        loading = true;
        notify();
        request(params, function(response) {
            if (requestSerial !== serial)
                return;
            inFlight = false;
            if (requestGeneration === generation) {
                loading = false;
                if (response.error) {
                    failed(response.error);
                } else {
                    var result = response.result || [];
                    var page = result.slice(0, pageSize);
                    hasMore = result.length > pageSize;
                    nextOffset = offset + page.length;
                    if (offset === 0) {
                        rows = page;
                    } else {
                        // New mail can shift offset pages while browsing.
                        var seen = {};
                        rows.forEach(function(row) { seen[row.id] = true; });
                        rows = rows.concat(page.filter(function(row) { return !seen[row.id]; }));
                    }
                    publish(rows);
                }
            }
            if (pendingRefresh) {
                pendingRefresh = false;
                fetchPage(0);
            } else {
                notify();
            }
        });
    }

    return {
        setFilters: setFilters,
        refresh: function(value) {
            setFilters(value);
            loading = true;
            if (inFlight) {
                pendingRefresh = true;
                notify();
            } else {
                fetchPage(0);
            }
        },
        loadMore: function() {
            if (!loading && !inFlight && hasMore)
                fetchPage(nextOffset);
        },
        reset: function() {
            serial++;
            generation++;
            inFlight = pendingRefresh = loading = hasMore = false;
            rows = [];
            nextOffset = 0;
            publish(rows);
            notify();
        }
    };
}

// Keep unchanged delegates alive across refreshes. JSON roles keep nested
// arrays (participants, labels) as JS arrays when QML reads each visible row.
function syncModel(model, rows) {
    for (var i = 0; i < rows.length; i++) {
        var row = rows[i], json = JSON.stringify(row);
        if (i >= model.count || model.get(i).rowId !== row.id) {
            var found = -1;
            for (var j = i + 1; j < model.count; j++) {
                if (model.get(j).rowId === row.id) { found = j; break; }
            }
            if (found < 0)
                model.insert(i, { rowId: row.id, rowJson: json });
            else
                model.move(found, i, 1);
        }
        if (model.get(i).rowJson !== json)
            model.setProperty(i, "rowJson", json);
    }
    if (model.count > rows.length)
        model.remove(rows.length, model.count - rows.length);
}
