"""One AF_INET control SSH22 connect; no read/write/auth/retry."""
import ipaddress, json, math, os, re, signal, socket, sys, time
UUID=re.compile(r'[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\Z')
class Invalid(ValueError): pass

def require(condition):
    if not condition: raise Invalid()

def pairs(items):
    result={}
    for key,value in items:
        require(key not in result); result[key]=value
    return result

def bad(_): raise Invalid()

def decode(raw,limit=4096):
    require(type(raw) is bytes and 0<len(raw)<=limit)
    try: return json.loads(raw.decode('utf-8','strict'),object_pairs_hook=pairs,parse_constant=bad)
    except (ValueError,RecursionError,UnicodeError): raise Invalid() from None

def exact(value,keys): require(type(value) is dict and set(value)==set(keys))

def binding(value):
    exact(value,('version','domain','session_id','backend_kind','backend_object','generation'))
    require(type(value['version']) is int and value['version']==1 and value['domain']=='n1qualification' and value['backend_kind']=='tart')
    for name in ('session_id','generation'): require(type(value[name]) is str and UUID.fullmatch(value[name]))
    require(value['session_id']!=value['generation'])
    require(type(value['backend_object']) is str and re.fullmatch(r'[A-Za-z0-9_-]{1,64}',value['backend_object']))
    return value


def connect(request,factory=socket.socket,clock=time.monotonic):
    exact(request,('binding','control_ipv4')); b=binding(request['binding'])
    value=request['control_ipv4']; require(type(value) is str and len(value)<=15)
    try: address=ipaddress.IPv4Address(value)
    except ValueError: raise Invalid() from None
    require(str(address)==value and any(address in ipaddress.IPv4Network(pool) for pool in ('10.0.0.0/8','172.16.0.0/12','192.168.0.0/16')))
    started=clock(); require(type(started) in (int,float) and math.isfinite(started))
    result={'version':1,'binding':b,'control_ipv4':value,'code':'socket_error','errno':0,'elapsed_us':0,'connected':False,'close_ok':True,'timing_ok':False}
    sock=None
    try:
        sock=factory(socket.AF_INET,socket.SOCK_STREAM); sock.settimeout(4.0)
        sock.connect((value,22)); result['code']='connected'; result['connected']=True
    except socket.timeout: result['code']='timeout'
    except OSError as error:
        result['errno']=error.errno if type(error.errno) is int and 0<=error.errno<=4095 else 0
    finally:
        if sock is not None:
            try: sock.close()
            except Exception: result['close_ok']=False
    try:
        ended=clock()
        result['timing_ok']=type(ended) in (int,float) and math.isfinite(ended) and 0<=ended-started<=10
        if result['timing_ok']: result['elapsed_us']=round((ended-started)*1000000)
    except Exception: pass
    return result

def main(argv=None,stdin=None,stdout=None):
    argv=sys.argv[1:] if argv is None else argv; stdin=sys.stdin.buffer if stdin is None else stdin; stdout=sys.stdout if stdout is None else stdout
    result={'version':1,'code':'invalid_binding'}; code=2
    try: require(argv==[]); result=connect(decode(stdin.read(4097))); code=0
    except Exception: pass
    stdout.write(json.dumps(result,separators=(',',':'))+'\n'); stdout.flush(); return code
if __name__=='__main__':
    signal.signal(signal.SIGALRM,signal.SIG_DFL); signal.alarm(10); raise SystemExit(main())
