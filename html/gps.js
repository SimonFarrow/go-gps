/* gps.js - Common java script code for gps */

/* jquery function for including html in web pages */
 $(function() {
	 $("#includeTopNav").load("topnav.html");
 });

/*  adds track with kml path as kml layer to passed html document id */
function addmap(kml,id) {        
	var myOptions = {          
		/* coords for Holme Head default centre for map */
		center: new google.maps.LatLng(54.083964, -2.280768),          
		zoom: 13,
		mapTypeId: google.maps.MapTypeId.TERRAIN        
	};        

	var map = new google.maps.Map( document.getElementById(id), myOptions);      
	var ctaLayer = new google.maps.KmlLayer(window.location.protocol + "//" + window.location.host + kml);
	ctaLayer.setMap(map);
}    
