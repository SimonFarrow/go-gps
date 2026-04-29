// uploads.js
// see https://uploadcare.com/blog/how-to-make-a-drag-and-drop-file-uploader/

const gpsIcon = 'favicon.ico';
const dropArea = document.getElementById('drop-area');
const fileInput = document.getElementById('file-input');


// Utility function to prevent default browser behavior
function preventDefaults(e) {
  e.preventDefault();
  e.stopPropagation();
}

// Preventing default browser behavior when dragging a file over the container
dropArea.addEventListener('dragenter', preventDefaults);

dropArea.addEventListener('dragover', (e) => {
  // this works
  e.preventDefault();
  // this did not
  // dropArea.classList.add('drag-over');
});

dropArea.addEventListener('dragleave', () => {
  dropArea.classList.remove('drag-over');
});

// Handling dropping files into the area
dropArea.addEventListener('drop', handleDrop);

function handleDrop(e) {
  e.preventDefault();

  // Getting the list of dragged files
  const files = e.dataTransfer.files;

  // Checking if there are any files
  if (files.length) {
    // Assigning the files to the hidden input from the first step
    fileInput.files = files;

    // Processing the files for previews (next step)
    handleFiles(files);
  }
}

function handleFiles(files) {
  for (const file of files) {
    // Initializing the FileReader API and reading the file
    const reader = new FileReader();
    //reader.readAsDataURL(file);
    reader.readAsText(file);

    // Once the file has been loaded, fire the processing
    reader.onloadend = function (e) {
      const preview = document.createElement('img');

      if (isValidFileExtension(file)) {
        preview.src = gpsIcon;
        preview.title = file.name;
        // Apply styling
        preview.classList.add('preview-image');
        const previewContainer = document.getElementById('preview-container');
        previewContainer.appendChild(preview);

        let formData = new FormData();
        formData.append("filename", file.name);
        formData.append("data", reader.result);

        fetch('/uploads', { method: "POST", body: formData })
          .then(response => {
            switch (response.status) {
              case 200:
                handle_response(response);
                break;
              case 403:
                if (confirm("File already exists, overwrite?")) {
                  formData.append("override", 'true');
                  fetch('/uploads', { method: "POST", body: formData })
                    .then(response => {
                      if (response.status == 200) {
                        handle_response(response);
                      } // if
                    }) // then
                    .catch(error => {
                      console.log("error " + error);
                    }) // catch
                }
                break;
            } // switch
          }) // then
          .catch(error => {
            console.log("error " + error);
          }) // catch
      } // isValidExtension
    } // onloadend
  } // for files
} // handleFiles

function handle_response(response) {
  // async process the json dictionary
  response.json().then(data => {
    const h1 = document.createElement('h1');
    h1.innerText = data.file_uploaded;
    const div = document.getElementById('results');
    div.insertBefore(h1, div.firstChild);
  })
}

function isValidFileExtension(file) {
  const extension = file.name.split('.').pop();
  const allowedExtensions = ['gpx', 'plt'];
  return allowedExtensions.includes(extension);
}
