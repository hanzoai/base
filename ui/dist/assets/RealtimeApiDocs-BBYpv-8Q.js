import{S as le,i as re,s as be,V as ue,W as me,J as B,j as o,d as oe,t as ne,a as ie,I as pe,n,o as y,m as ae,u,A as I,v as r,c as ce,w as m,b as de}from"./index-CzlWNNWT.js";function he(t){var U,W,A,L,P,H,T,j,q,J,M,z;let i,p,a=t[0].name+"",b,d,D,h,_,f,S,c,w,$,C,g,E,v,O,l,R;return c=new ue({props:{js:`
        import Base from 'base';

        const base = new Base('${t[1]}');

        ...

        // (Optionally) authenticate
        await base.collection('users').authWithPassword('test@example.com', '123456');

        // Subscribe to changes in any ${(U=t[0])==null?void 0:U.name} record
        base.collection('${(W=t[0])==null?void 0:W.name}').subscribe('*', function (e) {
            console.log(e.action);
            console.log(e.record);
        }, { /* other options like: filter, expand, custom headers, etc. */ });

        // Subscribe to changes only in the specified record
        base.collection('${(A=t[0])==null?void 0:A.name}').subscribe('RECORD_ID', function (e) {
            console.log(e.action);
            console.log(e.record);
        }, { /* other options like: filter, expand, custom headers, etc. */ });

        // Unsubscribe
        base.collection('${(L=t[0])==null?void 0:L.name}').unsubscribe('RECORD_ID'); // remove all 'RECORD_ID' subscriptions
        base.collection('${(P=t[0])==null?void 0:P.name}').unsubscribe('*'); // remove all '*' topic subscriptions
        base.collection('${(H=t[0])==null?void 0:H.name}').unsubscribe(); // remove all subscriptions in the collection
    `,dart:`
        import 'package:hanzoai/base.dart';

        final base = Base('${t[1]}');

        ...

        // (Optionally) authenticate
        await base.collection('users').authWithPassword('test@example.com', '123456');

        // Subscribe to changes in any ${(T=t[0])==null?void 0:T.name} record
        base.collection('${(j=t[0])==null?void 0:j.name}').subscribe('*', (e) {
            print(e.action);
            print(e.record);
        }, /* other options like: filter, expand, custom headers, etc. */);

        // Subscribe to changes only in the specified record
        base.collection('${(q=t[0])==null?void 0:q.name}').subscribe('RECORD_ID', (e) {
            print(e.action);
            print(e.record);
        }, /* other options like: filter, expand, custom headers, etc. */);

        // Unsubscribe
        base.collection('${(J=t[0])==null?void 0:J.name}').unsubscribe('RECORD_ID'); // remove all 'RECORD_ID' subscriptions
        base.collection('${(M=t[0])==null?void 0:M.name}').unsubscribe('*'); // remove all '*' topic subscriptions
        base.collection('${(z=t[0])==null?void 0:z.name}').unsubscribe(); // remove all subscriptions in the collection
    `}}),l=new me({props:{content:JSON.stringify({action:"create",record:B.dummyCollectionRecord(t[0])},null,2).replace('"action": "create"','"action": "create" // create, update or delete')}}),{c(){i=u("h3"),p=I("Realtime ("),b=I(a),d=I(")"),D=r(),h=u("div"),h.innerHTML=`<p>Subscribe to realtime changes via Server-Sent Events (SSE).</p> <p>Events are sent for <strong>create</strong>, <strong>update</strong>
        and <strong>delete</strong> record operations (see &quot;Event data format&quot; section below).</p>`,_=r(),f=u("div"),f.innerHTML=`<div class="icon"><i class="ri-information-line"></i></div> <div class="contet"><p><strong>You could subscribe to a single record or to an entire collection.</strong></p> <p>When you subscribe to a <strong>single record</strong>, the collection&#39;s
            <strong>ViewRule</strong> will be used to determine whether the subscriber has access to receive the
            event message.</p> <p>When you subscribe to an <strong>entire collection</strong>, the collection&#39;s
            <strong>ListRule</strong> will be used to determine whether the subscriber has access to receive the
            event message.</p></div>`,S=r(),ce(c.$$.fragment),w=r(),$=u("h6"),$.textContent="API details",C=r(),g=u("div"),g.innerHTML='<strong class="label label-primary">SSE</strong> <div class="content"><p>/api/realtime</p></div>',E=r(),v=u("div"),v.textContent="Event data format",O=r(),ce(l.$$.fragment),m(i,"class","m-b-sm"),m(h,"class","content txt-lg m-b-sm"),m(f,"class","alert alert-info m-t-10 m-b-sm"),m($,"class","m-b-xs"),m(g,"class","alert"),m(v,"class","section-title")},m(e,s){n(e,i,s),y(i,p),y(i,b),y(i,d),n(e,D,s),n(e,h,s),n(e,_,s),n(e,f,s),n(e,S,s),ae(c,e,s),n(e,w,s),n(e,$,s),n(e,C,s),n(e,g,s),n(e,E,s),n(e,v,s),n(e,O,s),ae(l,e,s),R=!0},p(e,[s]){var V,Y,F,G,K,Q,X,Z,x,ee,se,te;(!R||s&1)&&a!==(a=e[0].name+"")&&pe(b,a);const k={};s&3&&(k.js=`
        import Base from 'base';

        const base = new Base('${e[1]}');

        ...

        // (Optionally) authenticate
        await base.collection('users').authWithPassword('test@example.com', '123456');

        // Subscribe to changes in any ${(V=e[0])==null?void 0:V.name} record
        base.collection('${(Y=e[0])==null?void 0:Y.name}').subscribe('*', function (e) {
            console.log(e.action);
            console.log(e.record);
        }, { /* other options like: filter, expand, custom headers, etc. */ });

        // Subscribe to changes only in the specified record
        base.collection('${(F=e[0])==null?void 0:F.name}').subscribe('RECORD_ID', function (e) {
            console.log(e.action);
            console.log(e.record);
        }, { /* other options like: filter, expand, custom headers, etc. */ });

        // Unsubscribe
        base.collection('${(G=e[0])==null?void 0:G.name}').unsubscribe('RECORD_ID'); // remove all 'RECORD_ID' subscriptions
        base.collection('${(K=e[0])==null?void 0:K.name}').unsubscribe('*'); // remove all '*' topic subscriptions
        base.collection('${(Q=e[0])==null?void 0:Q.name}').unsubscribe(); // remove all subscriptions in the collection
    `),s&3&&(k.dart=`
        import 'package:hanzoai/base.dart';

        final base = Base('${e[1]}');

        ...

        // (Optionally) authenticate
        await base.collection('users').authWithPassword('test@example.com', '123456');

        // Subscribe to changes in any ${(X=e[0])==null?void 0:X.name} record
        base.collection('${(Z=e[0])==null?void 0:Z.name}').subscribe('*', (e) {
            print(e.action);
            print(e.record);
        }, /* other options like: filter, expand, custom headers, etc. */);

        // Subscribe to changes only in the specified record
        base.collection('${(x=e[0])==null?void 0:x.name}').subscribe('RECORD_ID', (e) {
            print(e.action);
            print(e.record);
        }, /* other options like: filter, expand, custom headers, etc. */);

        // Unsubscribe
        base.collection('${(ee=e[0])==null?void 0:ee.name}').unsubscribe('RECORD_ID'); // remove all 'RECORD_ID' subscriptions
        base.collection('${(se=e[0])==null?void 0:se.name}').unsubscribe('*'); // remove all '*' topic subscriptions
        base.collection('${(te=e[0])==null?void 0:te.name}').unsubscribe(); // remove all subscriptions in the collection
    `),c.$set(k);const N={};s&1&&(N.content=JSON.stringify({action:"create",record:B.dummyCollectionRecord(e[0])},null,2).replace('"action": "create"','"action": "create" // create, update or delete')),l.$set(N)},i(e){R||(ie(c.$$.fragment,e),ie(l.$$.fragment,e),R=!0)},o(e){ne(c.$$.fragment,e),ne(l.$$.fragment,e),R=!1},d(e){e&&(o(i),o(D),o(h),o(_),o(f),o(S),o(w),o($),o(C),o(g),o(E),o(v),o(O)),oe(c,e),oe(l,e)}}}function fe(t,i,p){let a,{collection:b}=i;return t.$$set=d=>{"collection"in d&&p(0,b=d.collection)},p(1,a=B.getApiExampleUrl(de.baseURL)),[b,a]}class ge extends le{constructor(i){super(),re(this,i,fe,he,be,{collection:0})}}export{ge as default};
