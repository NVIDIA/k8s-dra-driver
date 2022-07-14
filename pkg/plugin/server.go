package plugin

func StartRegistrationServer(driverName, draAddress, pluginRegistrationPath string) error {
	//regsitrar will register driver with Kubelet
	registrarConfig := nodeRegistrarConfig{
		draDriverName:          driverName,
		draAddress:             draAddress,
		pluginRegistrationPath: pluginRegistrationPath,
	}
	registrar := newRegistrar(registrarConfig)
	go registrar.nodeRegister()
	return nil
}
