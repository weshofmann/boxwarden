//! Exact diagnostic CLI grammar, shared by main and the neutral synthetic target.
use clap::Parser;
use super::wire::{
    Selector,mac
};
#[derive(Parser,Debug)]
#[command(version="0.19.0-boxwarden-n1-diagnostic.2")]
pub struct Args {
    #[arg(long)] pub vm_fd:i32,
    #[arg(long)] pub vm_mac_address:String,
    #[arg(long,default_value="nat")] pub vm_net_type:String,
    #[arg(long,default_value_t=600)] pub bootpd_lease_time:u32,
    #[arg(long)] pub user:Option<String>,
    #[arg(long)] pub group:Option<String>,
    #[arg(long,value_delimiter=',',action=clap::ArgAction::Set)] pub allow:Vec<String>,
    #[arg(long,value_delimiter=',',action=clap::ArgAction::Set,help="One exact @boxwarden-n1-diagnostic:<generation>:<nonce> selector")] pub block:Vec<String>,
    #[arg(long,value_delimiter=',',action=clap::ArgAction::Set)] pub expose:Vec<String>,
    #[arg(long,hide=true)] pub sudo_escalation_probing:bool,
    #[arg(long,hide=true)] pub sudo_escalation_done:bool,
}
impl Args {
    pub fn admit(&self)->Option<(Selector,[u8;6])>{
        if self.block.len()!=1||!self.allow.is_empty()||!self.expose.is_empty()||self.vm_net_type!="nat"||self.vm_fd!=0||self.bootpd_lease_time!=600||self.sudo_escalation_probing||self.sudo_escalation_done{
            return None;
        }
        let selector=Selector::parse(&self.block[0])?;
        let s=&self.vm_mac_address;
        if s.len()!=17||!s.is_ascii(){
            return None;
        }
        let mut out=[0u8;         6];
        for(i,v)in out.iter_mut().enumerate(){
            if i!=5&&s.as_bytes()[i*3+2]!=b':'{
                return None;
            }let p=&s[i*3..i*3+2];
            if !p.bytes().all(|b|b.is_ascii_digit()||(b'a'..=b'f').contains(&b)){
                return None;
            }*v=u8::from_str_radix(p,16).ok()?;
        }
        if !mac(out){
            return None;
        }Some((selector,out))
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    fn base()->Vec<String>{
        vec!["softnet".into(),"--vm-fd=0".into(),"--vm-mac-address=02:00:00:00:00:02".into(),format!("--block=@boxwarden-n1-diagnostic:{}:{}",crate::tests::arm().generation,crate::tests::arm().nonce)]
    }
    #[test]
    fn exact_child_argument_grammar_no_overrides_or_duplicate_selector(){
        let b=base();
        assert!(Args::try_parse_from(&b).unwrap().admit().is_some());
        for extra in ["--allow=0.0.0.0/0","--expose=2222:22","--vm-net-type=host","--vm-fd=-1","--bootpd-lease-time=601","--sudo-escalation-probing","--sudo-escalation-done"]{
            let mut a=b.clone();
            a.push(extra.into());
            assert!(Args::try_parse_from(a).map_or(true,|a|a.admit().is_none()));
        }
        let mut a=b.clone();
        a[3]="--block=@boxwarden-host-containment".into();
        assert!(Args::try_parse_from(&a).unwrap().admit().is_none());
        a[3]=format!("{},{}",b[3],b[3].strip_prefix("--block=").unwrap());
        assert!(Args::try_parse_from(a).unwrap().admit().is_none());
        let mut a=b.clone();
        a.push(b[3].clone());
        assert!(Args::try_parse_from(a).is_err());
        for s in ["02:00:00:00:00:0A","03:00:00:00:00:02","00:00:00:00:00:00"]{
            let mut a=b.clone();
            a[2]=format!("--vm-mac-address={s}");
            assert!(Args::try_parse_from(a).unwrap().admit().is_none());
        }
        // Exact one-element selector, no split embedded identities.
        assert_eq!(b.len(),4);
        assert!(!b[3].as_bytes().contains(&0));
    }
}
