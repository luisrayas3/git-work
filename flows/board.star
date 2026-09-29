def board(group_by="type"):
    """The shape of the work as a kanban: open stories, open decisions, and open tasks that belong to no story, a column per open status.

    The overview flow's query on a board. Open reads the status category,
    never the name (schema.yaml, D2): anything whose status is neither
    completed nor canceled. The columns are the open statuses alone, in
    schema order, because nothing closed is on the board to fill the others.
    A task under a story is that story's business and stays out of the way;
    one with no parent is on the board so it can be given one.
    """
    closed = []
    open_values = []
    for name, spec in work.schema.export()["types"].items():
        status = spec.get("fields", {}).get("status", {})
        ids = []
        for value in status.get("values", []):
            if value.get("category") in ("completed", "canceled"):
                ids.append(value["id"])
            elif value["id"] not in open_values:
                open_values.append(value["id"])
        closed.append('"%s":[%s]' % (name, ",".join(['"%s"' % id for id in ids])))

    query = """
        {%s} as $closed
        | map(select(
            (.fields.type == "story"
             or .fields.type == "decision"
             or (.fields.type == "task" and .fields.parent == null))
            and (.fields.status as $status
             | $closed[.fields.type] // [] | index($status) | not)))
    """ % ",".join(closed)

    work.view.board(
        query=query,
        columns="status",
        values=open_values,
        card=["title", "priority"],
        group_by=group_by,
    )
