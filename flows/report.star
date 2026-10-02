def report(from_="7d", to=None, iteration=None, query=None):
    """What changed since: created, closed, changed and commented, grouped by parent, as markdown.

    The window is [from, to): from_ and to are a date, an RFC 3339 time, or a
    duration back from now (7d, 2w, 12h), and an absent `to` is now. An
    iteration id takes the window from that issue's start and end fields
    instead, which is why an iteration is not a time form: which fields hold
    an iteration's dates is policy, and policy lives here (doc/design/report.md).

    `from_` carries a trailing underscore because `from` is a reserved word in
    Starlark, the same reason `work.schema.import_` does.

    The set is `query`, a jq program over the issues, run at both ends of the
    window, so an issue archived during it is reported rather than dropped.
    The default is every unarchived issue.

    What is said about each issue is this flow's choice, not the tool's: there
    are no field roles, so `status` and `parent` are named here, in the open.
    Only issues that actually changed are printed.
    """

    def short(id):
        return id[:7]

    def by_id(items):
        out = {}
        for item in items:
            out[item["id"]] = item
        return out

    def show(value):
        if value == None:
            return "(none)"
        if type(value) == "string":
            return value
        if type(value) == "list":
            parts = []
            for element in value:
                if type(element) == "string":
                    parts.append(element)
                else:
                    parts.append(str(element))
            return "[" + ", ".join(parts) + "]"
        return str(value)

    def named_value(value):
        # A relation holds another issue's id, so draw it as that issue; any
        # other full-length id — a person, say — is shortened, because a
        # status report is read, not parsed.
        if type(value) == "string":
            if value in after:
                return "%s %s" % (short(value), show(after[value]["fields"].get("title")))
            if len(value) == 64:
                return short(value)
        return show(value)

    def first_paragraph(text, limit):
        head = text.split("\n\n")[0]
        head = " ".join(head.split())
        if len(head) > limit:
            return head[:limit].rstrip() + "…"
        return head

    # The window. An iteration names an issue whose dates are the window;
    # without them there is nothing to read, so say so and keep from_/to.
    window_from = from_
    window_to = to
    if iteration != None:
        dates = work.issue.get(iteration)["fields"]
        start = dates.get("start")
        end = dates.get("end")
        if start == None:
            work.stderr("report: iteration %s has no start date; using from_=%s" % (short(iteration), from_))
        else:
            window_from = start
            window_to = end
            if end == None:
                work.stderr("report: iteration %s has no end date; the window runs to now" % short(iteration))

    selector = query
    if selector == None:
        selector = "map(select(.fields.archived != true))"

    # Every issue at each end of the window, for the before/after comparison
    # and for a parent's title; the selection at each end, so that an issue
    # that left the selection during the window is still reported.
    before = by_id(work.issue.list(".", at=window_from))
    after = by_id(work.issue.list(".", at=window_to))

    picked = {}
    for item in work.issue.list(selector, at=window_from):
        picked[item["id"]] = True
    for item in work.issue.list(selector, at=window_to):
        picked[item["id"]] = True

    # Every operation in the window, grouped by the issue it belongs to.
    operations = {}
    for entry in work.issue.log(".", from_=window_from, to=window_to):
        operations.setdefault(entry["issue"], []).append(entry)

    # Which statuses mean closed, read from the schema's categories and never
    # from a status name (schema.yaml, D2). A status id is read against its
    # own type first, because a field belongs to exactly one type.
    closed_by_type = {}
    closed_anywhere = {}
    for type_key, spec in work.schema.export()["types"].items():
        status = spec.get("fields", {}).get("status", {})
        closed_by_type[type_key] = {}
        for value in status.get("values", []):
            if value.get("category") in ("completed", "canceled"):
                closed_by_type[type_key][value["id"]] = True
                closed_anywhere[value["id"]] = True

    def is_closed(item, status):
        if status == None:
            return False
        type_key = item["fields"].get("type")
        if type_key != None and type_key in closed_by_type:
            return status in closed_by_type[type_key]
        return status in closed_anywhere

    created = 0
    closed = 0
    changed = 0
    commented = 0
    blocks = {}

    for id in picked:
        old = before.get(id)
        new = after.get(id)
        if new == None:
            continue

        marks = []
        lines = []

        if old == None:
            marks.append("created")
            created += 1

        old_fields = {}
        if old != None:
            old_fields = old["fields"]
        new_fields = new["fields"]

        if is_closed(new, new_fields.get("status")) and not is_closed(new, old_fields.get("status")):
            marks.append("closed")
            closed += 1
        if new_fields.get("archived") == True and old_fields.get("archived") != True:
            marks.append("archived")

        # Every changed field, because this flow does not know which ones
        # matter and a missing line is worse than an extra one. The status
        # carries the whole path it took, read from the operations: the states
        # a task passed through are often the interesting part of a week.
        keys = {}
        for key in old_fields:
            keys[key] = True
        for key in new_fields:
            keys[key] = True

        path = []
        comments = []
        for entry in operations.get(id, []):
            if entry["type"] == "set-field" and entry["op"]["key"] == "status":
                path.append(show(entry["op"]["value"]))
            elif entry["type"] == "add-comment":
                comments.append(entry)

        field_lines = 0
        for key in sorted(keys):
            was = old_fields.get(key)
            now = new_fields.get(key)
            if was == now:
                continue
            if key == "title" and was == None:
                # The header already carries it, and a created issue's title
                # was never anything else.
                continue
            line = "  %s: %s → %s" % (key, named_value(was), named_value(now))
            if key == "status" and len(path) > 1:
                line += " (via %s)" % ", ".join(path[:-1])
            lines.append(line)
            field_lines += 1

        if field_lines > 0 and old != None:
            marks.append("changed")
            changed += 1

        for entry in comments:
            lines.append("  comment — %s, %s: %s" % (
                entry["author"]["name"],
                entry["time"][:10],
                first_paragraph(entry["op"]["message"], 200),
            ))
        if len(comments) > 0:
            commented += 1

        if len(marks) == 0 and len(lines) == 0:
            continue

        suffix = ""
        if len(marks) > 0:
            suffix = " — " + ", ".join(marks)
        block = ["- %s %s%s" % (short(id), show(new_fields.get("title")), suffix)]
        for line in lines:
            block.append(line)

        parent = new_fields.get("parent")
        if type(parent) != "string":
            parent = ""
        blocks.setdefault(parent, []).append(block)

    # Groups in title order, the parentless last, because they are the odds
    # and ends rather than the body of the week.
    named = []
    for parent in blocks:
        if parent == "":
            continue
        title = short(parent)
        if parent in after:
            title = show(after[parent]["fields"].get("title"))
        named.append((title, parent))

    print("# What changed, %s → %s" % (window_from, window_to or "now"))
    print("")
    print("%d created · %d closed · %d changed · %d commented" % (created, closed, changed, commented))

    for pair in sorted(named):
        print("")
        print("## %s" % pair[0])
        print("")
        for block in blocks[pair[1]]:
            for line in block:
                print(line)

    if "" in blocks:
        print("")
        print("## (none)")
        print("")
        for block in blocks[""]:
            for line in block:
                print(line)

    return None
