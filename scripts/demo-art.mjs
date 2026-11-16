// Original procedural illustrations. No downloaded images, fonts, or existing characters.
export const titles = ['The Lantern Atlas', 'Cloudline Courier', 'The Quiet Observatory', 'Moss & Mercury', 'Tideglass Station', 'Paper Moon Orchard', 'The Copper Heron', 'Winter Signal', 'A Garden Above', 'The Last Tram Home', 'Inkstone Harbor', 'Small Hours Society'];
export function drawArtwork(index, page, volume, title) {
  const c = document.createElement('canvas'); c.width = 900; c.height = 1280;
  const g = c.getContext('2d');
  const palettes = [['#172f46','#376875','#efa66a'],['#253956','#7297b6','#f1cf85'],['#282c4a','#72779a','#d6b7a2'],['#203e38','#72907b','#e8c08d'],['#173e50','#679b9e','#efb895'],['#473b51','#a58499','#efcfaa']];
  const [ink, mid, light] = palettes[index % palettes.length];
  const rect=(x,y,w,h,color)=>{g.fillStyle=color;g.fillRect(x,y,w,h);};
  const line=(x,y,X,Y,color=ink,width=3)=>{g.strokeStyle=color;g.lineWidth=width;g.beginPath();g.moveTo(x,y);g.lineTo(X,Y);g.stroke();};
  const circle=(x,y,r,color)=>{g.fillStyle=color;g.beginPath();g.arc(x,y,r,0,Math.PI*2);g.fill();};
  const text=(s,x,y,size,color,font='serif')=>{g.fillStyle=color;g.font=`${size}px ${font}`;g.fillText(s,x,y);};
  function scene(x,y,w,h,variant) {
    g.save();g.beginPath();g.rect(x,y,w,h);g.clip();g.translate(x,y);g.scale(w/900,h/1000);
    const sky=g.createLinearGradient(0,0,0,1000);sky.addColorStop(0,ink);sky.addColorStop(1,mid);g.fillStyle=sky;g.fillRect(0,0,900,1000);
    const moonX=490+(index*47)%240;circle(moonX,160,70+(index%4)*12,light);circle(moonX+30,132,65+(index%4)*12,ink);
    if(index%4===1){g.strokeStyle=light;g.lineWidth=4;g.beginPath();g.ellipse(moonX,160,155,30,-.3,0,7);g.stroke();}
    if(index%4===2){for(let k=0;k<8;k++){const a=k*Math.PI/4;line(moonX+115*Math.cos(a),160+115*Math.sin(a),moonX+130*Math.cos(a),160+130*Math.sin(a),light,3);}}
    if(index%4===3){for(let k=0;k<4;k++){g.strokeStyle=light;g.lineWidth=2;g.beginPath();g.arc(120,100,38+k*23,0,Math.PI);g.stroke();}}
    for(let j=0;j<50;j++)circle((j*173+index*23)%900,(j*79)%470,1.4,'#e6d6b5');
    // Distant terraced city, bridges, glowing windows and cables.
    for(let j=0;j<15;j++) {const X=j*75-50; const H=170+(j*97+index*67)%300;rect(X,710-H,65,H,mid);g.strokeStyle=ink;g.lineWidth=3;g.strokeRect(X,710-H,65,H);for(let k=0;k<5;k++)for(let a=0;a<3;a++)rect(X+10+a*17,730-H+k*35,6,13,light);line(X-10,710-H,X+33,670-H);line(X+33,670-H,X+75,710-H);}
    g.fillStyle=ink;g.beginPath();g.moveTo(0,790);g.quadraticCurveTo(450,590,900,780);g.lineTo(900,1000);g.lineTo(0,1000);g.fill();
    for(let j=0;j<12;j++){line(j*90,740,j*90,850,light,3);line(0,745,900,745,light,3);}
    for(let j=0;j<9;j++)line(450,730,j*170-250,1000,mid,2);
    // Lanterns and their reflections.
    for(let j=0;j<4;j++){let X=90+j*230;line(X,0,X,260+j*33,light,2);circle(X,280+j*33,22,light);line(X-12,280+j*33,X+12,280+j*33,ink,2);}
    // Original traveler: windswept scarf, coat, satchel and rolled star map.
    const px=330+variant*48+(volume-1)*35;circle(px,599,29,light);g.fillStyle=ink;g.beginPath();g.arc(px-3,586,29,Math.PI,Math.PI*2);g.fill();
    g.fillStyle=light;g.beginPath();g.moveTo(px-26,633);g.lineTo(px+25,633);g.lineTo(px+50,792);g.lineTo(px-52,792);g.closePath();g.fill();
    line(px-18,790,px-25,874,light,16);line(px+23,790,px+37,875,light,16);line(px-38,878,px-12,878,ink,9);line(px+25,878,px+53,878,ink,9);
    line(px-23,641,px+38,735,ink,6);rect(px+12,718,51,46,mid);g.strokeStyle=ink;g.strokeRect(px+12,718,51,46);
    g.fillStyle=light;g.beginPath();g.moveTo(px+12,630);g.bezierCurveTo(px+90,610,px+110,670,px+184,610);g.lineTo(px+143,650);g.bezierCurveTo(px+80,690,px+70,636,px+12,646);g.fill();
    line(px-30,661,px-72,712,light,15);line(px-75,697,px-50,724,ink,7);
    // Foreground inked leaves.
    for(let j=0;j<16;j++){const X=(j%2?860:30)+(j%3)*8,Y=900-j*22;line(X,1000,X,Y,light,2);g.fillStyle=mid;g.beginPath();g.ellipse(X+(j%2?-18:18),Y,27,9,j%2?-.6:.6,0,7);g.fill();}
    g.restore();
  }
  rect(0,0,900,1280,'#f3e5cc');
  if(page===0){scene(0,0,900,1280,(index+volume)%4);rect(40,38,820,3,light);text('Y O M I F O L I O   /   O R I G I N A L S',64,88,19,light,'sans-serif');
    const words=title.split(' ');const split=Math.ceil(words.length/2);text(words.slice(0,split).join(' '),62,205,65,'#fff1d6');text(words.slice(split).join(' '),62,281,65,'#fff1d6');
    rect(52,1147,796,89,ink);text('ROWAN VALE',76,1202,25,light,'sans-serif');text(`VOL. 0${volume}`,666,1202,25,light,'sans-serif');
  }else{
    text('THE LANTERN ATLAS',38,42,18,ink,'sans-serif');text(`CHAPTER ${volume} / THE CITY THAT LISTENS`,400,42,16,ink,'sans-serif');
    scene(35,65,830,440,page%3);scene(35,525,398,340,2);scene(453,525,412,340,0);scene(35,885,830,340,1);
    // Hand-lettered narrative captions and speech, integral to the original comic page.
    const captions=['At dusk, the bridges began to sing.','The map had forgotten this street.','Beyond the roofs, a signal flickered.','Someone had left a lantern burning.','The city held its breath for us.','At last, the tide answered back.'];
    const speech=['Which way is the sea?','Perhaps we should listen.','There. Above the old station.','You kept the light alive.','Can you hear it too?','We can find our way home.'];
    const closing=['I followed the smallest light.','Not every road needs a name.','One more bridge. One more step.','A small kindness travels far.','Even silence has a current.','Tomorrow, a new map begins.'];
    rect(60,91,550,60,'#f3e5cc');text(captions[(page-1)%6],77,129,23,ink);
    g.fillStyle='#fff7e5';g.beginPath();g.ellipse(648,592,167,43,0,0,7);g.fill();text(speech[(page-1)%6],494,600,21,ink);
    rect(64,920,480,57,'#f3e5cc');text(closing[(page-1)%6],80,956,23,ink);
    text(String(page+1).padStart(2,'0'),826,1260,18,ink,'sans-serif');
  }
  return c.toDataURL('image/png').split(',')[1];
}
