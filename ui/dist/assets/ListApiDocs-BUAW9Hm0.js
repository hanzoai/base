import{S as tl,i as ll,s as sl,H as Ut,j as m,n as h,p as nl,u as t,v as s,L as ol,w as a,o as e,A as g,V as al,W as Lt,X as at,d as Ke,Y as il,t as Ce,a as ye,I as kt,Z as Jt,_ as rl,C as cl,$ as dl,D as fl,m as Qe,c as Ve,J as Tt,b as pl,l as At}from"./index-CzlWNNWT.js";import{F as ul}from"./FieldsQueryParam-BjMsFtDw.js";function ml(r){let n,o,i;return{c(){n=t("span"),n.textContent="Show details",o=s(),i=t("i"),a(n,"class","txt"),a(i,"class","ri-arrow-down-s-line")},m(p,b){h(p,n,b),h(p,o,b),h(p,i,b)},d(p){p&&(m(n),m(o),m(i))}}}function hl(r){let n,o,i;return{c(){n=t("span"),n.textContent="Hide details",o=s(),i=t("i"),a(n,"class","txt"),a(i,"class","ri-arrow-up-s-line")},m(p,b){h(p,n,b),h(p,o,b),h(p,i,b)},d(p){p&&(m(n),m(o),m(i))}}}function Kt(r){let n,o,i,p,b,f,u,C,_,x,d,Y,ve,We,N,Xe,D,ie,R,Z,it,j,z,rt,re,ke,ee,Fe,ct,ce,de,te,P,Ye,Le,y,le,Ae,Ze,Te,U,se,Re,et,Oe,k,fe,Se,dt,pe,ft,H,Ee,ne,Ne,F,ue,pt,J,Pe,tt,qe,lt,De,ut,L,me,mt,he,ht,M,bt,T,He,oe,Me,K,be,gt,I,Ie,v,Be,ae,Ge,_t,Q,ge,wt,_e,xt,je,$t,B,ze,Ct,G,yt,we,st,O,xe,V,W,S,Ue,nt,X;return{c(){n=t("p"),n.innerHTML=`The syntax basically follows the format
        <code><span class="txt-success">OPERAND</span> <span class="txt-danger">OPERATOR</span> <span class="txt-success">OPERAND</span></code>, where:`,o=s(),i=t("ul"),p=t("li"),p.innerHTML=`<code class="txt-success">OPERAND</code> - could be any of the above field literal, string (single
            or double quoted), number, null, true, false`,b=s(),f=t("li"),u=t("code"),u.textContent="OPERATOR",C=g(` - is one of:
            `),_=t("br"),x=s(),d=t("ul"),Y=t("li"),ve=t("code"),ve.textContent="=",We=s(),N=t("span"),N.textContent="Equal",Xe=s(),D=t("li"),ie=t("code"),ie.textContent="!=",R=s(),Z=t("span"),Z.textContent="NOT equal",it=s(),j=t("li"),z=t("code"),z.textContent=">",rt=s(),re=t("span"),re.textContent="Greater than",ke=s(),ee=t("li"),Fe=t("code"),Fe.textContent=">=",ct=s(),ce=t("span"),ce.textContent="Greater than or equal",de=s(),te=t("li"),P=t("code"),P.textContent="<",Ye=s(),Le=t("span"),Le.textContent="Less than",y=s(),le=t("li"),Ae=t("code"),Ae.textContent="<=",Ze=s(),Te=t("span"),Te.textContent="Less than or equal",U=s(),se=t("li"),Re=t("code"),Re.textContent="~",et=s(),Oe=t("span"),Oe.textContent=`Like/Contains (if not specified auto wraps the right string OPERAND in a "%" for
                        wildcard match)`,k=s(),fe=t("li"),Se=t("code"),Se.textContent="!~",dt=s(),pe=t("span"),pe.textContent=`NOT Like/Contains (if not specified auto wraps the right string OPERAND in a "%" for
                        wildcard match)`,ft=s(),H=t("li"),Ee=t("code"),Ee.textContent="?=",ne=s(),Ne=t("em"),Ne.textContent="Any/At least one of",F=s(),ue=t("span"),ue.textContent="Equal",pt=s(),J=t("li"),Pe=t("code"),Pe.textContent="?!=",tt=s(),qe=t("em"),qe.textContent="Any/At least one of",lt=s(),De=t("span"),De.textContent="NOT equal",ut=s(),L=t("li"),me=t("code"),me.textContent="?>",mt=s(),he=t("em"),he.textContent="Any/At least one of",ht=s(),M=t("span"),M.textContent="Greater than",bt=s(),T=t("li"),He=t("code"),He.textContent="?>=",oe=s(),Me=t("em"),Me.textContent="Any/At least one of",K=s(),be=t("span"),be.textContent="Greater than or equal",gt=s(),I=t("li"),Ie=t("code"),Ie.textContent="?<",v=s(),Be=t("em"),Be.textContent="Any/At least one of",ae=s(),Ge=t("span"),Ge.textContent="Less than",_t=s(),Q=t("li"),ge=t("code"),ge.textContent="?<=",wt=s(),_e=t("em"),_e.textContent="Any/At least one of",xt=s(),je=t("span"),je.textContent="Less than or equal",$t=s(),B=t("li"),ze=t("code"),ze.textContent="?~",Ct=s(),G=t("em"),G.textContent="Any/At least one of",yt=s(),we=t("span"),we.textContent=`Like/Contains (if not specified auto wraps the right string OPERAND in a "%" for
                        wildcard match)`,st=s(),O=t("li"),xe=t("code"),xe.textContent="?!~",V=s(),W=t("em"),W.textContent="Any/At least one of",S=s(),Ue=t("span"),Ue.textContent=`NOT Like/Contains (if not specified auto wraps the right string OPERAND in a "%" for
                        wildcard match)`,nt=s(),X=t("p"),X.innerHTML=`To group and combine several expressions you could use brackets
        <code>(...)</code>, <code>&amp;&amp;</code> (AND) and <code>||</code> (OR) tokens.`,a(u,"class","txt-danger"),a(ve,"class","filter-op svelte-1w7s5nw"),a(N,"class","txt"),a(ie,"class","filter-op svelte-1w7s5nw"),a(Z,"class","txt"),a(z,"class","filter-op svelte-1w7s5nw"),a(re,"class","txt"),a(Fe,"class","filter-op svelte-1w7s5nw"),a(ce,"class","txt"),a(P,"class","filter-op svelte-1w7s5nw"),a(Le,"class","txt"),a(Ae,"class","filter-op svelte-1w7s5nw"),a(Te,"class","txt"),a(Re,"class","filter-op svelte-1w7s5nw"),a(Oe,"class","txt"),a(Se,"class","filter-op svelte-1w7s5nw"),a(pe,"class","txt"),a(Ee,"class","filter-op svelte-1w7s5nw"),a(Ne,"class","txt-hint"),a(ue,"class","txt"),a(Pe,"class","filter-op svelte-1w7s5nw"),a(qe,"class","txt-hint"),a(De,"class","txt"),a(me,"class","filter-op svelte-1w7s5nw"),a(he,"class","txt-hint"),a(M,"class","txt"),a(He,"class","filter-op svelte-1w7s5nw"),a(Me,"class","txt-hint"),a(be,"class","txt"),a(Ie,"class","filter-op svelte-1w7s5nw"),a(Be,"class","txt-hint"),a(Ge,"class","txt"),a(ge,"class","filter-op svelte-1w7s5nw"),a(_e,"class","txt-hint"),a(je,"class","txt"),a(ze,"class","filter-op svelte-1w7s5nw"),a(G,"class","txt-hint"),a(we,"class","txt"),a(xe,"class","filter-op svelte-1w7s5nw"),a(W,"class","txt-hint"),a(Ue,"class","txt")},m($,$e){h($,n,$e),h($,o,$e),h($,i,$e),e(i,p),e(i,b),e(i,f),e(f,u),e(f,C),e(f,_),e(f,x),e(f,d),e(d,Y),e(Y,ve),e(Y,We),e(Y,N),e(d,Xe),e(d,D),e(D,ie),e(D,R),e(D,Z),e(d,it),e(d,j),e(j,z),e(j,rt),e(j,re),e(d,ke),e(d,ee),e(ee,Fe),e(ee,ct),e(ee,ce),e(d,de),e(d,te),e(te,P),e(te,Ye),e(te,Le),e(d,y),e(d,le),e(le,Ae),e(le,Ze),e(le,Te),e(d,U),e(d,se),e(se,Re),e(se,et),e(se,Oe),e(d,k),e(d,fe),e(fe,Se),e(fe,dt),e(fe,pe),e(d,ft),e(d,H),e(H,Ee),e(H,ne),e(H,Ne),e(H,F),e(H,ue),e(d,pt),e(d,J),e(J,Pe),e(J,tt),e(J,qe),e(J,lt),e(J,De),e(d,ut),e(d,L),e(L,me),e(L,mt),e(L,he),e(L,ht),e(L,M),e(d,bt),e(d,T),e(T,He),e(T,oe),e(T,Me),e(T,K),e(T,be),e(d,gt),e(d,I),e(I,Ie),e(I,v),e(I,Be),e(I,ae),e(I,Ge),e(d,_t),e(d,Q),e(Q,ge),e(Q,wt),e(Q,_e),e(Q,xt),e(Q,je),e(d,$t),e(d,B),e(B,ze),e(B,Ct),e(B,G),e(B,yt),e(B,we),e(d,st),e(d,O),e(O,xe),e(O,V),e(O,W),e(O,S),e(O,Ue),h($,nt,$e),h($,X,$e)},d($){$&&(m(n),m(o),m(i),m(nt),m(X))}}}function bl(r){let n,o,i,p,b;function f(x,d){return x[0]?hl:ml}let u=f(r),C=u(r),_=r[0]&&Kt();return{c(){n=t("button"),C.c(),o=s(),_&&_.c(),i=ol(),a(n,"class","btn btn-sm btn-secondary m-t-10")},m(x,d){h(x,n,d),C.m(n,null),h(x,o,d),_&&_.m(x,d),h(x,i,d),p||(b=nl(n,"click",r[1]),p=!0)},p(x,[d]){u!==(u=f(x))&&(C.d(1),C=u(x),C&&(C.c(),C.m(n,null))),x[0]?_||(_=Kt(),_.c(),_.m(i.parentNode,i)):_&&(_.d(1),_=null)},i:Ut,o:Ut,d(x){x&&(m(n),m(o),m(i)),C.d(),_&&_.d(x),p=!1,b()}}}function gl(r,n,o){let i=!1;function p(){o(0,i=!i)}return[i,p]}class _l extends tl{constructor(n){super(),ll(this,n,gl,bl,sl,{})}}function Qt(r,n,o){const i=r.slice();return i[8]=n[o],i}function Vt(r,n,o){const i=r.slice();return i[8]=n[o],i}function Wt(r,n,o){const i=r.slice();return i[13]=n[o],i[15]=o,i}function Xt(r){let n;return{c(){n=t("p"),n.innerHTML="Requires superuser <code>Authorization:TOKEN</code> header",a(n,"class","txt-hint txt-sm txt-right")},m(o,i){h(o,n,i)},d(o){o&&m(n)}}}function Yt(r){let n,o=r[13]+"",i,p=r[15]<r[4].length-1?", ":"",b;return{c(){n=t("code"),i=g(o),b=g(p)},m(f,u){h(f,n,u),e(n,i),h(f,b,u)},p(f,u){u&16&&o!==(o=f[13]+"")&&kt(i,o),u&16&&p!==(p=f[15]<f[4].length-1?", ":"")&&kt(b,p)},d(f){f&&(m(n),m(b))}}}function Zt(r,n){let o,i,p;function b(){return n[7](n[8])}return{key:r,first:null,c(){o=t("button"),o.textContent=`${n[8].code} `,a(o,"type","button"),a(o,"class","tab-item"),At(o,"active",n[2]===n[8].code),this.first=o},m(f,u){h(f,o,u),i||(p=nl(o,"click",b),i=!0)},p(f,u){n=f,u&36&&At(o,"active",n[2]===n[8].code)},d(f){f&&m(o),i=!1,p()}}}function el(r,n){let o,i,p,b;return i=new Lt({props:{content:n[8].body}}),{key:r,first:null,c(){o=t("div"),Ve(i.$$.fragment),p=s(),a(o,"class","tab-item"),At(o,"active",n[2]===n[8].code),this.first=o},m(f,u){h(f,o,u),Qe(i,o,null),e(o,p),b=!0},p(f,u){n=f,(!b||u&36)&&At(o,"active",n[2]===n[8].code)},i(f){b||(ye(i.$$.fragment,f),b=!0)},o(f){Ce(i.$$.fragment,f),b=!1},d(f){f&&m(o),Ke(i)}}}function wl(r){var St,Et,Nt,Pt,qt,Dt;let n,o,i=r[0].name+"",p,b,f,u,C,_,x,d=r[0].name+"",Y,ve,We,N,Xe,D,ie,R,Z,it,j,z,rt,re,ke=r[0].name+"",ee,Fe,ct,ce,de,te,P,Ye,Le,y,le,Ae,Ze,Te,U,se,Re,et,Oe,k,fe,Se,dt,pe,ft,H,Ee,ne,Ne,F,ue,pt,J,Pe,tt,qe,lt,De,ut,L,me,mt,he,ht,M,bt,T,He,oe,Me,K,be,gt,I,Ie,v,Be,ae,Ge,_t,Q,ge,wt,_e,xt,je,$t,B,ze,Ct,G,yt,we,st,O,xe,V,W,S=[],Ue=new Map,nt,X,$=[],$e=new Map,Je;N=new al({props:{js:`
        import Base from 'base';

        const base = new Base('${r[3]}');

        ...

        // fetch a paginated records list
        const resultList = await base.collection('${(St=r[0])==null?void 0:St.name}').getList(1, 50, {
            filter: 'someField1 != someField2',
        });

        // you can also fetch all records at once via getFullList
        const records = await base.collection('${(Et=r[0])==null?void 0:Et.name}').getFullList({
            sort: '-someField',
        });

        // or fetch only the first record that matches the specified filter
        const record = await base.collection('${(Nt=r[0])==null?void 0:Nt.name}').getFirstListItem('someField="test"', {
            expand: 'relField1,relField2.subRelField',
        });
    `,dart:`
        import 'package:hanzoai/base.dart';

        final base = Base('${r[3]}');

        ...

        // fetch a paginated records list
        final resultList = await base.collection('${(Pt=r[0])==null?void 0:Pt.name}').getList(
          page: 1,
          perPage: 50,
          filter: 'someField1 != someField2',
        );

        // you can also fetch all records at once via getFullList
        final records = await base.collection('${(qt=r[0])==null?void 0:qt.name}').getFullList(
          sort: '-someField',
        );

        // or fetch only the first record that matches the specified filter
        final record = await base.collection('${(Dt=r[0])==null?void 0:Dt.name}').getFirstListItem(
          'someField="test"',
          expand: 'relField1,relField2.subRelField',
        );
    `}});let E=r[1]&&Xt();ne=new Lt({props:{content:`
                        // DESC by created and ASC by id
                        ?sort=-created,id
                    `}});let ot=at(r[4]),A=[];for(let l=0;l<ot.length;l+=1)A[l]=Yt(Wt(r,ot,l));T=new Lt({props:{content:`
                        ?filter=(id='abc' && created>'2022-01-01')
                    `}}),oe=new _l({}),ae=new Lt({props:{content:"?expand=relField1,relField2.subRelField"}}),G=new ul({});let Ft=at(r[5]);const Rt=l=>l[8].code;for(let l=0;l<Ft.length;l+=1){let c=Vt(r,Ft,l),w=Rt(c);Ue.set(w,S[l]=Zt(w,c))}let vt=at(r[5]);const Ot=l=>l[8].code;for(let l=0;l<vt.length;l+=1){let c=Qt(r,vt,l),w=Ot(c);$e.set(w,$[l]=el(w,c))}return{c(){n=t("h3"),o=g("List/Search ("),p=g(i),b=g(")"),f=s(),u=t("div"),C=t("p"),_=g("Fetch a paginated "),x=t("strong"),Y=g(d),ve=g(" records list, supporting sorting and filtering."),We=s(),Ve(N.$$.fragment),Xe=s(),D=t("h6"),D.textContent="API details",ie=s(),R=t("div"),Z=t("strong"),Z.textContent="GET",it=s(),j=t("div"),z=t("p"),rt=g("/api/collections/"),re=t("strong"),ee=g(ke),Fe=g("/records"),ct=s(),E&&E.c(),ce=s(),de=t("div"),de.textContent="Query parameters",te=s(),P=t("table"),Ye=t("thead"),Ye.innerHTML='<tr><th>Param</th> <th>Type</th> <th width="60%">Description</th></tr>',Le=s(),y=t("tbody"),le=t("tr"),le.innerHTML='<td>page</td> <td><span class="label">Number</span></td> <td>The page (aka. offset) of the paginated list (default to 1).</td>',Ae=s(),Ze=t("tr"),Ze.innerHTML='<td>perPage</td> <td><span class="label">Number</span></td> <td>Specify the max returned records per page (default to 30).</td>',Te=s(),U=t("tr"),se=t("td"),se.textContent="sort",Re=s(),et=t("td"),et.innerHTML='<span class="label">String</span>',Oe=s(),k=t("td"),fe=g("Specify the records order attribute(s). "),Se=t("br"),dt=g(`
                Add `),pe=t("code"),pe.textContent="-",ft=g(" / "),H=t("code"),H.textContent="+",Ee=g(` (default) in front of the attribute for DESC / ASC order.
                Ex.:
                `),Ve(ne.$$.fragment),Ne=s(),F=t("p"),ue=t("strong"),ue.textContent="Supported record sort fields:",pt=s(),J=t("br"),Pe=s(),tt=t("code"),tt.textContent="@random",qe=g(`,
                    `),lt=t("code"),lt.textContent="@rowid",De=g(`,
                    `);for(let l=0;l<A.length;l+=1)A[l].c();ut=s(),L=t("tr"),me=t("td"),me.textContent="filter",mt=s(),he=t("td"),he.innerHTML='<span class="label">String</span>',ht=s(),M=t("td"),bt=g(`Filter the returned records. Ex.:
                `),Ve(T.$$.fragment),He=s(),Ve(oe.$$.fragment),Me=s(),K=t("tr"),be=t("td"),be.textContent="expand",gt=s(),I=t("td"),I.innerHTML='<span class="label">String</span>',Ie=s(),v=t("td"),Be=g(`Auto expand record relations. Ex.:
                `),Ve(ae.$$.fragment),Ge=g(`
                Supports up to 6-levels depth nested relations expansion. `),_t=t("br"),Q=g(`
                The expanded relations will be appended to each individual record under the
                `),ge=t("code"),ge.textContent="expand",wt=g(" property (eg. "),_e=t("code"),_e.textContent='"expand": {"relField1": {...}, ...}',xt=g(`).
                `),je=t("br"),$t=g(`
                Only the relations to which the request user has permissions to `),B=t("strong"),B.textContent="view",ze=g(" will be expanded."),Ct=s(),Ve(G.$$.fragment),yt=s(),we=t("tr"),we.innerHTML=`<td id="query-page">skipTotal</td> <td><span class="label">Boolean</span></td> <td>If it is set the total counts query will be skipped and the response fields
                <code>totalItems</code> and <code>totalPages</code> will have <code>-1</code> value.
                <br/>
                This could drastically speed up the search queries when the total counters are not needed or cursor
                based pagination is used.
                <br/>
                For optimization purposes, it is set by default for the
                <code>getFirstListItem()</code>
                and
                <code>getFullList()</code> SDKs methods.</td>`,st=s(),O=t("div"),O.textContent="Responses",xe=s(),V=t("div"),W=t("div");for(let l=0;l<S.length;l+=1)S[l].c();nt=s(),X=t("div");for(let l=0;l<$.length;l+=1)$[l].c();a(n,"class","m-b-sm"),a(u,"class","content txt-lg m-b-sm"),a(D,"class","m-b-xs"),a(Z,"class","label label-primary"),a(j,"class","content"),a(R,"class","alert alert-info"),a(de,"class","section-title"),a(P,"class","table-compact table-border m-b-base"),a(O,"class","section-title"),a(W,"class","tabs-header compact combined left"),a(X,"class","tabs-content"),a(V,"class","tabs")},m(l,c){h(l,n,c),e(n,o),e(n,p),e(n,b),h(l,f,c),h(l,u,c),e(u,C),e(C,_),e(C,x),e(x,Y),e(C,ve),h(l,We,c),Qe(N,l,c),h(l,Xe,c),h(l,D,c),h(l,ie,c),h(l,R,c),e(R,Z),e(R,it),e(R,j),e(j,z),e(z,rt),e(z,re),e(re,ee),e(z,Fe),e(R,ct),E&&E.m(R,null),h(l,ce,c),h(l,de,c),h(l,te,c),h(l,P,c),e(P,Ye),e(P,Le),e(P,y),e(y,le),e(y,Ae),e(y,Ze),e(y,Te),e(y,U),e(U,se),e(U,Re),e(U,et),e(U,Oe),e(U,k),e(k,fe),e(k,Se),e(k,dt),e(k,pe),e(k,ft),e(k,H),e(k,Ee),Qe(ne,k,null),e(k,Ne),e(k,F),e(F,ue),e(F,pt),e(F,J),e(F,Pe),e(F,tt),e(F,qe),e(F,lt),e(F,De);for(let w=0;w<A.length;w+=1)A[w]&&A[w].m(F,null);e(y,ut),e(y,L),e(L,me),e(L,mt),e(L,he),e(L,ht),e(L,M),e(M,bt),Qe(T,M,null),e(M,He),Qe(oe,M,null),e(y,Me),e(y,K),e(K,be),e(K,gt),e(K,I),e(K,Ie),e(K,v),e(v,Be),Qe(ae,v,null),e(v,Ge),e(v,_t),e(v,Q),e(v,ge),e(v,wt),e(v,_e),e(v,xt),e(v,je),e(v,$t),e(v,B),e(v,ze),e(y,Ct),Qe(G,y,null),e(y,yt),e(y,we),h(l,st,c),h(l,O,c),h(l,xe,c),h(l,V,c),e(V,W);for(let w=0;w<S.length;w+=1)S[w]&&S[w].m(W,null);e(V,nt),e(V,X);for(let w=0;w<$.length;w+=1)$[w]&&$[w].m(X,null);Je=!0},p(l,[c]){var Ht,Mt,It,Bt,Gt,jt;(!Je||c&1)&&i!==(i=l[0].name+"")&&kt(p,i),(!Je||c&1)&&d!==(d=l[0].name+"")&&kt(Y,d);const w={};if(c&9&&(w.js=`
        import Base from 'base';

        const base = new Base('${l[3]}');

        ...

        // fetch a paginated records list
        const resultList = await base.collection('${(Ht=l[0])==null?void 0:Ht.name}').getList(1, 50, {
            filter: 'someField1 != someField2',
        });

        // you can also fetch all records at once via getFullList
        const records = await base.collection('${(Mt=l[0])==null?void 0:Mt.name}').getFullList({
            sort: '-someField',
        });

        // or fetch only the first record that matches the specified filter
        const record = await base.collection('${(It=l[0])==null?void 0:It.name}').getFirstListItem('someField="test"', {
            expand: 'relField1,relField2.subRelField',
        });
    `),c&9&&(w.dart=`
        import 'package:hanzoai/base.dart';

        final base = Base('${l[3]}');

        ...

        // fetch a paginated records list
        final resultList = await base.collection('${(Bt=l[0])==null?void 0:Bt.name}').getList(
          page: 1,
          perPage: 50,
          filter: 'someField1 != someField2',
        );

        // you can also fetch all records at once via getFullList
        final records = await base.collection('${(Gt=l[0])==null?void 0:Gt.name}').getFullList(
          sort: '-someField',
        );

        // or fetch only the first record that matches the specified filter
        final record = await base.collection('${(jt=l[0])==null?void 0:jt.name}').getFirstListItem(
          'someField="test"',
          expand: 'relField1,relField2.subRelField',
        );
    `),N.$set(w),(!Je||c&1)&&ke!==(ke=l[0].name+"")&&kt(ee,ke),l[1]?E||(E=Xt(),E.c(),E.m(R,null)):E&&(E.d(1),E=null),c&16){ot=at(l[4]);let q;for(q=0;q<ot.length;q+=1){const zt=Wt(l,ot,q);A[q]?A[q].p(zt,c):(A[q]=Yt(zt),A[q].c(),A[q].m(F,null))}for(;q<A.length;q+=1)A[q].d(1);A.length=ot.length}c&36&&(Ft=at(l[5]),S=Jt(S,c,Rt,1,l,Ft,Ue,W,rl,Zt,null,Vt)),c&36&&(vt=at(l[5]),cl(),$=Jt($,c,Ot,1,l,vt,$e,X,dl,el,null,Qt),fl())},i(l){if(!Je){ye(N.$$.fragment,l),ye(ne.$$.fragment,l),ye(T.$$.fragment,l),ye(oe.$$.fragment,l),ye(ae.$$.fragment,l),ye(G.$$.fragment,l);for(let c=0;c<vt.length;c+=1)ye($[c]);Je=!0}},o(l){Ce(N.$$.fragment,l),Ce(ne.$$.fragment,l),Ce(T.$$.fragment,l),Ce(oe.$$.fragment,l),Ce(ae.$$.fragment,l),Ce(G.$$.fragment,l);for(let c=0;c<$.length;c+=1)Ce($[c]);Je=!1},d(l){l&&(m(n),m(f),m(u),m(We),m(Xe),m(D),m(ie),m(R),m(ce),m(de),m(te),m(P),m(st),m(O),m(xe),m(V)),Ke(N,l),E&&E.d(),Ke(ne),il(A,l),Ke(T),Ke(oe),Ke(ae),Ke(G);for(let c=0;c<S.length;c+=1)S[c].d();for(let c=0;c<$.length;c+=1)$[c].d()}}}function xl(r,n,o){let i,p,b,f,{collection:u}=n,C=200,_=[];const x=d=>o(2,C=d.code);return r.$$set=d=>{"collection"in d&&o(0,u=d.collection)},r.$$.update=()=>{r.$$.dirty&1&&o(4,i=Tt.getAllCollectionIdentifiers(u)),r.$$.dirty&1&&o(1,p=(u==null?void 0:u.listRule)===null),r.$$.dirty&1&&o(6,f=Tt.dummyCollectionRecord(u)),r.$$.dirty&67&&u!=null&&u.id&&(_.push({code:200,body:JSON.stringify({page:1,perPage:30,totalPages:1,totalItems:2,items:[f,Object.assign({},f,{id:f.id+"2"})]},null,2)}),_.push({code:400,body:`
                {
                  "status": 400,
                  "message": "Something went wrong while processing your request. Invalid filter.",
                  "data": {}
                }
            `}),p&&_.push({code:403,body:`
                    {
                      "status": 403,
                      "message": "Only superusers can access this action.",
                      "data": {}
                    }
                `}))},o(3,b=Tt.getApiExampleUrl(pl.baseURL)),[u,p,C,b,i,_,f,x]}class yl extends tl{constructor(n){super(),ll(this,n,xl,wl,sl,{collection:0})}}export{yl as default};
