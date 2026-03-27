import{S as Re,i as Ee,s as Te,V as Be,X as P,j as f,d as De,t as te,a as le,I as ee,Z as he,_ as Ie,C as Oe,$ as Ae,D as Me,n as m,o as i,m as Ce,u as r,A as $,v as k,c as we,w as p,J as qe,b as Le,l as U,p as Se,W as je}from"./index-CzlWNNWT.js";function ke(a,l,s){const n=a.slice();return n[6]=l[s],n}function ge(a,l,s){const n=a.slice();return n[6]=l[s],n}function ve(a){let l;return{c(){l=r("p"),l.innerHTML="Requires superuser <code>Authorization:TOKEN</code> header",p(l,"class","txt-hint txt-sm txt-right")},m(s,n){m(s,l,n)},d(s){s&&f(l)}}}function $e(a,l){let s,n,h;function c(){return l[5](l[6])}return{key:a,first:null,c(){s=r("button"),s.textContent=`${l[6].code} `,p(s,"class","tab-item"),U(s,"active",l[2]===l[6].code),this.first=s},m(o,d){m(o,s,d),n||(h=Se(s,"click",c),n=!0)},p(o,d){l=o,d&20&&U(s,"active",l[2]===l[6].code)},d(o){o&&f(s),n=!1,h()}}}function ye(a,l){let s,n,h,c;return n=new je({props:{content:l[6].body}}),{key:a,first:null,c(){s=r("div"),we(n.$$.fragment),h=k(),p(s,"class","tab-item"),U(s,"active",l[2]===l[6].code),this.first=s},m(o,d){m(o,s,d),Ce(n,s,null),i(s,h),c=!0},p(o,d){l=o,(!c||d&20)&&U(s,"active",l[2]===l[6].code)},i(o){c||(le(n.$$.fragment,o),c=!0)},o(o){te(n.$$.fragment,o),c=!1},d(o){o&&f(s),De(n)}}}function ze(a){var me,pe;let l,s,n=a[0].name+"",h,c,o,d,y,D,F,L=a[0].name+"",J,se,K,C,N,T,V,g,S,ae,j,E,ne,W,z=a[0].name+"",X,oe,Z,ie,G,B,Q,I,Y,O,x,w,A,v=[],re=new Map,ce,M,_=[],de=new Map,R;C=new Be({props:{js:`
        import Base from 'base';

        const base = new Base('${a[3]}');

        ...

        await base.collection('${(me=a[0])==null?void 0:me.name}').delete('RECORD_ID');
    `,dart:`
        import 'package:hanzoai/base.dart';

        final base = Base('${a[3]}');

        ...

        await base.collection('${(pe=a[0])==null?void 0:pe.name}').delete('RECORD_ID');
    `}});let b=a[1]&&ve(),H=P(a[4]);const ue=e=>e[6].code;for(let e=0;e<H.length;e+=1){let t=ge(a,H,e),u=ue(t);re.set(u,v[e]=$e(u,t))}let q=P(a[4]);const fe=e=>e[6].code;for(let e=0;e<q.length;e+=1){let t=ke(a,q,e),u=fe(t);de.set(u,_[e]=ye(u,t))}return{c(){l=r("h3"),s=$("Delete ("),h=$(n),c=$(")"),o=k(),d=r("div"),y=r("p"),D=$("Delete a single "),F=r("strong"),J=$(L),se=$(" record."),K=k(),we(C.$$.fragment),N=k(),T=r("h6"),T.textContent="API details",V=k(),g=r("div"),S=r("strong"),S.textContent="DELETE",ae=k(),j=r("div"),E=r("p"),ne=$("/api/collections/"),W=r("strong"),X=$(z),oe=$("/records/"),Z=r("strong"),Z.textContent=":id",ie=k(),b&&b.c(),G=k(),B=r("div"),B.textContent="Path parameters",Q=k(),I=r("table"),I.innerHTML='<thead><tr><th>Param</th> <th>Type</th> <th width="60%">Description</th></tr></thead> <tbody><tr><td>id</td> <td><span class="label">String</span></td> <td>ID of the record to delete.</td></tr></tbody>',Y=k(),O=r("div"),O.textContent="Responses",x=k(),w=r("div"),A=r("div");for(let e=0;e<v.length;e+=1)v[e].c();ce=k(),M=r("div");for(let e=0;e<_.length;e+=1)_[e].c();p(l,"class","m-b-sm"),p(d,"class","content txt-lg m-b-sm"),p(T,"class","m-b-xs"),p(S,"class","label label-primary"),p(j,"class","content"),p(g,"class","alert alert-danger"),p(B,"class","section-title"),p(I,"class","table-compact table-border m-b-base"),p(O,"class","section-title"),p(A,"class","tabs-header compact combined left"),p(M,"class","tabs-content"),p(w,"class","tabs")},m(e,t){m(e,l,t),i(l,s),i(l,h),i(l,c),m(e,o,t),m(e,d,t),i(d,y),i(y,D),i(y,F),i(F,J),i(y,se),m(e,K,t),Ce(C,e,t),m(e,N,t),m(e,T,t),m(e,V,t),m(e,g,t),i(g,S),i(g,ae),i(g,j),i(j,E),i(E,ne),i(E,W),i(W,X),i(E,oe),i(E,Z),i(g,ie),b&&b.m(g,null),m(e,G,t),m(e,B,t),m(e,Q,t),m(e,I,t),m(e,Y,t),m(e,O,t),m(e,x,t),m(e,w,t),i(w,A);for(let u=0;u<v.length;u+=1)v[u]&&v[u].m(A,null);i(w,ce),i(w,M);for(let u=0;u<_.length;u+=1)_[u]&&_[u].m(M,null);R=!0},p(e,[t]){var _e,be;(!R||t&1)&&n!==(n=e[0].name+"")&&ee(h,n),(!R||t&1)&&L!==(L=e[0].name+"")&&ee(J,L);const u={};t&9&&(u.js=`
        import Base from 'base';

        const base = new Base('${e[3]}');

        ...

        await base.collection('${(_e=e[0])==null?void 0:_e.name}').delete('RECORD_ID');
    `),t&9&&(u.dart=`
        import 'package:hanzoai/base.dart';

        final base = Base('${e[3]}');

        ...

        await base.collection('${(be=e[0])==null?void 0:be.name}').delete('RECORD_ID');
    `),C.$set(u),(!R||t&1)&&z!==(z=e[0].name+"")&&ee(X,z),e[1]?b||(b=ve(),b.c(),b.m(g,null)):b&&(b.d(1),b=null),t&20&&(H=P(e[4]),v=he(v,t,ue,1,e,H,re,A,Ie,$e,null,ge)),t&20&&(q=P(e[4]),Oe(),_=he(_,t,fe,1,e,q,de,M,Ae,ye,null,ke),Me())},i(e){if(!R){le(C.$$.fragment,e);for(let t=0;t<q.length;t+=1)le(_[t]);R=!0}},o(e){te(C.$$.fragment,e);for(let t=0;t<_.length;t+=1)te(_[t]);R=!1},d(e){e&&(f(l),f(o),f(d),f(K),f(N),f(T),f(V),f(g),f(G),f(B),f(Q),f(I),f(Y),f(O),f(x),f(w)),De(C,e),b&&b.d();for(let t=0;t<v.length;t+=1)v[t].d();for(let t=0;t<_.length;t+=1)_[t].d()}}}function He(a,l,s){let n,h,{collection:c}=l,o=204,d=[];const y=D=>s(2,o=D.code);return a.$$set=D=>{"collection"in D&&s(0,c=D.collection)},a.$$.update=()=>{a.$$.dirty&1&&s(1,n=(c==null?void 0:c.deleteRule)===null),a.$$.dirty&3&&c!=null&&c.id&&(d.push({code:204,body:`
                null
            `}),d.push({code:400,body:`
                {
                  "status": 400,
                  "message": "Failed to delete record. Make sure that the record is not part of a required relation reference.",
                  "data": {}
                }
            `}),n&&d.push({code:403,body:`
                    {
                      "status": 403,
                      "message": "Only superusers can access this action.",
                      "data": {}
                    }
                `}),d.push({code:404,body:`
                {
                  "status": 404,
                  "message": "The requested resource wasn't found.",
                  "data": {}
                }
            `}))},s(3,h=qe.getApiExampleUrl(Le.baseURL)),[c,n,o,h,d,y]}class Ue extends Re{constructor(l){super(),Ee(this,l,He,ze,Te,{collection:0})}}export{Ue as default};
