from .core import Command, Group

def command(name=None, cls=Command):
    def decorator(f):
        return cls()
    return decorator

def group(name=None, cls=Group):
    def decorator(f):
        return cls()
    return decorator
