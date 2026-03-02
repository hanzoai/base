import{S as gt,i as vt,s as kt,V as St,X as L,W as _t,j as d,d as ae,Y as wt,t as K,a as X,I as Z,Z as dt,_ as yt,C as $t,$ as Ct,D as Rt,n as c,o as t,m as se,u as o,A as f,v as h,c as oe,w as g,J as ct,b as Ot,l as ne,p as Pt}from"./index-CzlWNNWT.js";import{F as Tt}from"./FieldsQueryParam-BjMsFtDw.js";function ut(i,s,a){const n=i.slice();return n[7]=s[a],n}function ht(i,s,a){const n=i.slice();return n[7]=s[a],n}function bt(i,s,a){const n=i.slice();return n[12]=s[a],n[14]=a,n}function At(i){let s;return{c(){s=f("or")},m(a,n){c(a,s,n)},d(a){a&&d(s)}}}function pt(i){let s,a,n=i[12]+"",m,p=i[14]>0&&At();return{c(){p&&p.c(),s=h(),a=o("strong"),m=f(n)},m(r,b){p&&p.m(r,b),c(r,s,b),c(r,a,b),t(a,m)},p(r,b){b&2&&n!==(n=r[12]+"")&&Z(m,n)},d(r){r&&(d(s),d(a)),p&&p.d(r)}}}function ft(i,s){let a,n=s[7].code+"",m,p,r,b;function v(){return s[6](s[7])}return{key:i,first:null,c(){a=o("button"),m=f(n),p=h(),g(a,"class","tab-item"),ne(a,"active",s[2]===s[7].code),this.first=a},m($,_){c($,a,_),t(a,m),t(a,p),r||(b=Pt(a,"click",v),r=!0)},p($,_){s=$,_&8&&n!==(n=s[7].code+"")&&Z(m,n),_&12&&ne(a,"active",s[2]===s[7].code)},d($){$&&d(a),r=!1,b()}}}function mt(i,s){let a,n,m,p;return n=new _t({props:{content:s[7].body}}),{key:i,first:null,c(){a=o("div"),oe(n.$$.fragment),m=h(),g(a,"class","tab-item"),ne(a,"active",s[2]===s[7].code),this.first=a},m(r,b){c(r,a,b),se(n,a,null),t(a,m),p=!0},p(r,b){s=r;const v={};b&8&&(v.content=s[7].body),n.$set(v),(!p||b&12)&&ne(a,"active",s[2]===s[7].code)},i(r){p||(X(n.$$.fragment,r),p=!0)},o(r){K(n.$$.fragment,r),p=!1},d(r){r&&d(a),ae(n)}}}function Dt(i){var st,ot;let s,a,n=i[0].name+"",m,p,r,b,v,$,_,G=i[1].join("/")+"",ie,De,re,We,de,R,ce,q,ue,O,x,Fe,ee,H,Me,he,te=i[0].name+"",be,Ue,pe,j,fe,P,me,Be,Y,T,_e,Le,ge,qe,V,ve,He,ke,Se,E,we,A,ye,je,N,D,$e,Ye,Ce,Ve,k,Ee,M,Ne,Ie,Je,Re,ze,Oe,Qe,Ke,Xe,Pe,Ze,Ge,U,Te,I,Ae,W,J,C=[],xe=new Map,et,z,w=[],tt=new Map,F;R=new St({props:{js:`
        import Base from 'base';

        const base = new Base('${i[5]}');

        ...

        const authData = await base.collection('${(st=i[0])==null?void 0:st.name}').authWithPassword(
            '${i[4]}',
            'YOUR_PASSWORD',
        );

        // after the above you can also access the auth data from the authStore
        console.log(base.authStore.isValid);
        console.log(base.authStore.token);
        console.log(base.authStore.record.id);

        // "logout"
        base.authStore.clear();
    `,dart:`
        import 'package:hanzoai/base.dart';

        final base = Base('${i[5]}');

        ...

        final authData = await base.collection('${(ot=i[0])==null?void 0:ot.name}').authWithPassword(
          '${i[4]}',
          'YOUR_PASSWORD',
        );

        // after the above you can also access the auth data from the authStore
        print(base.authStore.isValid);
        print(base.authStore.token);
        print(base.authStore.record.id);

        // "logout"
        base.authStore.clear();
    `}});let B=L(i[1]),S=[];for(let e=0;e<B.length;e+=1)S[e]=pt(bt(i,B,e));M=new _t({props:{content:"?expand=relField1,relField2.subRelField"}}),U=new Tt({props:{prefix:"record."}});let le=L(i[3]);const lt=e=>e[7].code;for(let e=0;e<le.length;e+=1){let l=ht(i,le,e),u=lt(l);xe.set(u,C[e]=ft(u,l))}let Q=L(i[3]);const at=e=>e[7].code;for(let e=0;e<Q.length;e+=1){let l=ut(i,Q,e),u=at(l);tt.set(u,w[e]=mt(u,l))}return{c(){s=o("h3"),a=f("Auth with password ("),m=f(n),p=f(")"),r=h(),b=o("div"),v=o("p"),$=f(`Authenticate with combination of
        `),_=o("strong"),ie=f(G),De=f(" and "),re=o("strong"),re.textContent="password",We=f("."),de=h(),oe(R.$$.fragment),ce=h(),q=o("h6"),q.textContent="API details",ue=h(),O=o("div"),x=o("strong"),x.textContent="POST",Fe=h(),ee=o("div"),H=o("p"),Me=f("/api/collections/"),he=o("strong"),be=f(te),Ue=f("/auth-with-password"),pe=h(),j=o("div"),j.textContent="Body Parameters",fe=h(),P=o("table"),me=o("thead"),me.innerHTML='<tr><th>Param</th> <th>Type</th> <th width="50%">Description</th></tr>',Be=h(),Y=o("tbody"),T=o("tr"),_e=o("td"),_e.innerHTML='<div class="inline-flex"><span class="label label-success">Required</span> <span>identity</span></div>',Le=h(),ge=o("td"),ge.innerHTML='<span class="label">String</span>',qe=h(),V=o("td");for(let e=0;e<S.length;e+=1)S[e].c();ve=f(`
                of the record to authenticate.`),He=h(),ke=o("tr"),ke.innerHTML='<td><div class="inline-flex"><span class="label label-success">Required</span> <span>password</span></div></td> <td><span class="label">String</span></td> <td>The auth record password.</td>',Se=h(),E=o("div"),E.textContent="Query parameters",we=h(),A=o("table"),ye=o("thead"),ye.innerHTML='<tr><th>Param</th> <th>Type</th> <th width="60%">Description</th></tr>',je=h(),N=o("tbody"),D=o("tr"),$e=o("td"),$e.textContent="expand",Ye=h(),Ce=o("td"),Ce.innerHTML='<span class="label">String</span>',Ve=h(),k=o("td"),Ee=f(`Auto expand record relations. Ex.:
                `),oe(M.$$.fragment),Ne=f(`
                Supports up to 6-levels depth nested relations expansion. `),Ie=o("br"),Je=f(`
                The expanded relations will be appended to the record under the
                `),Re=o("code"),Re.textContent="expand",ze=f(" property (eg. "),Oe=o("code"),Oe.textContent='"expand": {"relField1": {...}, ...}',Qe=f(`).
                `),Ke=o("br"),Xe=f(`
                Only the relations to which the request user has permissions to `),Pe=o("strong"),Pe.textContent="view",Ze=f(" will be expanded."),Ge=h(),oe(U.$$.fragment),Te=h(),I=o("div"),I.textContent="Responses",Ae=h(),W=o("div"),J=o("div");for(let e=0;e<C.length;e+=1)C[e].c();et=h(),z=o("div");for(let e=0;e<w.length;e+=1)w[e].c();g(s,"class","m-b-sm"),g(b,"class","content txt-lg m-b-sm"),g(q,"class","m-b-xs"),g(x,"class","label label-primary"),g(ee,"class","content"),g(O,"class","alert alert-success"),g(j,"class","section-title"),g(P,"class","table-compact table-border m-b-base"),g(E,"class","section-title"),g(A,"class","table-compact table-border m-b-base"),g(I,"class","section-title"),g(J,"class","tabs-header compact combined left"),g(z,"class","tabs-content"),g(W,"class","tabs")},m(e,l){c(e,s,l),t(s,a),t(s,m),t(s,p),c(e,r,l),c(e,b,l),t(b,v),t(v,$),t(v,_),t(_,ie),t(v,De),t(v,re),t(v,We),c(e,de,l),se(R,e,l),c(e,ce,l),c(e,q,l),c(e,ue,l),c(e,O,l),t(O,x),t(O,Fe),t(O,ee),t(ee,H),t(H,Me),t(H,he),t(he,be),t(H,Ue),c(e,pe,l),c(e,j,l),c(e,fe,l),c(e,P,l),t(P,me),t(P,Be),t(P,Y),t(Y,T),t(T,_e),t(T,Le),t(T,ge),t(T,qe),t(T,V);for(let u=0;u<S.length;u+=1)S[u]&&S[u].m(V,null);t(V,ve),t(Y,He),t(Y,ke),c(e,Se,l),c(e,E,l),c(e,we,l),c(e,A,l),t(A,ye),t(A,je),t(A,N),t(N,D),t(D,$e),t(D,Ye),t(D,Ce),t(D,Ve),t(D,k),t(k,Ee),se(M,k,null),t(k,Ne),t(k,Ie),t(k,Je),t(k,Re),t(k,ze),t(k,Oe),t(k,Qe),t(k,Ke),t(k,Xe),t(k,Pe),t(k,Ze),t(N,Ge),se(U,N,null),c(e,Te,l),c(e,I,l),c(e,Ae,l),c(e,W,l),t(W,J);for(let u=0;u<C.length;u+=1)C[u]&&C[u].m(J,null);t(W,et),t(W,z);for(let u=0;u<w.length;u+=1)w[u]&&w[u].m(z,null);F=!0},p(e,[l]){var nt,it;(!F||l&1)&&n!==(n=e[0].name+"")&&Z(m,n),(!F||l&2)&&G!==(G=e[1].join("/")+"")&&Z(ie,G);const u={};if(l&49&&(u.js=`
        import Base from 'base';

        const base = new Base('${e[5]}');

        ...

        const authData = await base.collection('${(nt=e[0])==null?void 0:nt.name}').authWithPassword(
            '${e[4]}',
            'YOUR_PASSWORD',
        );

        // after the above you can also access the auth data from the authStore
        console.log(base.authStore.isValid);
        console.log(base.authStore.token);
        console.log(base.authStore.record.id);

        // "logout"
        base.authStore.clear();
    `),l&49&&(u.dart=`
        import 'package:hanzoai/base.dart';

        final base = Base('${e[5]}');

        ...

        final authData = await base.collection('${(it=e[0])==null?void 0:it.name}').authWithPassword(
          '${e[4]}',
          'YOUR_PASSWORD',
        );

        // after the above you can also access the auth data from the authStore
        print(base.authStore.isValid);
        print(base.authStore.token);
        print(base.authStore.record.id);

        // "logout"
        base.authStore.clear();
    `),R.$set(u),(!F||l&1)&&te!==(te=e[0].name+"")&&Z(be,te),l&2){B=L(e[1]);let y;for(y=0;y<B.length;y+=1){const rt=bt(e,B,y);S[y]?S[y].p(rt,l):(S[y]=pt(rt),S[y].c(),S[y].m(V,ve))}for(;y<S.length;y+=1)S[y].d(1);S.length=B.length}l&12&&(le=L(e[3]),C=dt(C,l,lt,1,e,le,xe,J,yt,ft,null,ht)),l&12&&(Q=L(e[3]),$t(),w=dt(w,l,at,1,e,Q,tt,z,Ct,mt,null,ut),Rt())},i(e){if(!F){X(R.$$.fragment,e),X(M.$$.fragment,e),X(U.$$.fragment,e);for(let l=0;l<Q.length;l+=1)X(w[l]);F=!0}},o(e){K(R.$$.fragment,e),K(M.$$.fragment,e),K(U.$$.fragment,e);for(let l=0;l<w.length;l+=1)K(w[l]);F=!1},d(e){e&&(d(s),d(r),d(b),d(de),d(ce),d(q),d(ue),d(O),d(pe),d(j),d(fe),d(P),d(Se),d(E),d(we),d(A),d(Te),d(I),d(Ae),d(W)),ae(R,e),wt(S,e),ae(M),ae(U);for(let l=0;l<C.length;l+=1)C[l].d();for(let l=0;l<w.length;l+=1)w[l].d()}}}function Wt(i,s,a){let n,m,p,{collection:r}=s,b=200,v=[];const $=_=>a(2,b=_.code);return i.$$set=_=>{"collection"in _&&a(0,r=_.collection)},i.$$.update=()=>{var _;i.$$.dirty&1&&a(1,m=((_=r==null?void 0:r.passwordAuth)==null?void 0:_.identityFields)||[]),i.$$.dirty&2&&a(4,p=m.length==0?"NONE":"YOUR_"+m.join("_OR_").toUpperCase()),i.$$.dirty&1&&a(3,v=[{code:200,body:JSON.stringify({token:"JWT_TOKEN",record:ct.dummyCollectionRecord(r)},null,2)},{code:400,body:`
                {
                  "status": 400,
                  "message": "Failed to authenticate.",
                  "data": {
                    "identity": {
                      "code": "validation_required",
                      "message": "Missing required value."
                    }
                  }
                }
            `}])},a(5,n=ct.getApiExampleUrl(Ot.baseURL)),[r,m,b,v,p,n,$]}class Ut extends gt{constructor(s){super(),vt(this,s,Wt,Dt,kt,{collection:0})}}export{Ut as default};
