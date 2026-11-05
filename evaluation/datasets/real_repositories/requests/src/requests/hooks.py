HOOKS = ["response"]
def default_hooks():
    return {event: [] for event in HOOKS}
